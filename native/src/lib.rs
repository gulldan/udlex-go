//! C ABI над `udlex-rs`. Решений о лемматизации здесь нет: функции разбирают
//! аргументы, зовут те же методы крейта, что и Python-биндинг, и кладут ответ в
//! буфер вызывающего.
//!
//! * Пакет приходит одним буфером байт плюс `count + 1` смещений: один переход
//!   границы на предложение.
//! * Память под ответ даёт вызывающий; не хватило — [`UDLEX_MORE_SPACE`] и
//!   точный размер.
//! * Лемма, равная входу, не копируется: флаг `out_same` (или [`UDLEX_SAME`])
//!   разрешает вызывающему вернуть свою строку.
//!
//! Ошибка — отрицательный код и текст во владеемом буфере `err`, который
//! освобождает [`udlex_bytes_free`].

use std::panic::{AssertUnwindSafe, catch_unwind};
use std::path::Path;

use udlex_rs::{Lexicon, Options, RuntimeLexicon, Source, UserDictionary};

// --- Коды возврата -------------------------------------------------------

/// Ответ записан в выходной буфер.
pub const UDLEX_OK: i32 = 0;
/// Ответ побайтово равен входу; в выходной буфер ничего не записано.
pub const UDLEX_SAME: i32 = 1;
/// Выходного буфера не хватило; нужный размер записан в `out_len`.
pub const UDLEX_MORE_SPACE: i32 = 2;

/// Аргумент нарушает контракт: нулевой указатель, ломаные смещения, не та длина.
pub const UDLEX_ERR_ARG: i32 = -1;
/// Вход не UTF-8 или граница токена рассекает символ.
pub const UDLEX_ERR_UTF8: i32 = -2;
/// Словарь не открылся: нет каталога, битая таблица, несовместимый формат.
pub const UDLEX_ERR_OPEN: i32 = -3;
/// Поверхность спана пуста, из одних пробелов или с краевым пробелом.
pub const UDLEX_ERR_SURFACE: i32 = -4;
/// Таблица именованных сущностей повреждена.
pub const UDLEX_ERR_ENTITIES: i32 = -5;
/// Паника внутри библиотеки — перехвачена, чтобы не разматывать стек через C.
pub const UDLEX_ERR_PANIC: i32 = -6;

// --- Биты флагов вызова --------------------------------------------------

/// Токен открывает предложение.
pub const UDLEX_SENTENCE_INITIAL: u8 = 1;
/// Вернуть поверхность без единого поиска.
pub const UDLEX_PROTECTED: u8 = 2;
/// Узкий entity-профиль вместо обычной цепочки проб.
pub const UDLEX_ENTITY: u8 = 4;

/// Владеемый библиотекой буфер: текст ошибки или JSON каталога.
///
/// Освобождается только через [`udlex_bytes_free`] — аллокатор Rust не
/// обязан совпадать с аллокатором вызывающего.
#[repr(C)]
#[derive(Debug)]
pub struct UdlexBytes {
    /// Начало буфера; null, если буфер пуст.
    pub ptr: *mut u8,
    /// Длина в байтах.
    pub len: usize,
}

impl UdlexBytes {
    const EMPTY: Self = Self {
        ptr: std::ptr::null_mut(),
        len: 0,
    };

    fn owned(text: String) -> Self {
        let boxed = text.into_bytes().into_boxed_slice();
        let len = boxed.len();
        if len == 0 {
            return Self::EMPTY;
        }
        Self {
            ptr: Box::into_raw(boxed).cast::<u8>(),
            len,
        }
    }
}

/// Какие таблицы лексикону разрешено спрашивать; зеркало `udlex_rs::Options`.
///
/// Ноль — «нет», любое ненулевое — «да». Указатель `NULL` вместо структуры
/// означает профиль по умолчанию, тот же, что у `udlex.load` без аргументов.
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct UdlexOptions {
    /// Точная таблица имён собственных.
    pub use_entities: u8,
    /// Она же по ключу в нижнем регистре; [`UDLEX_PROBE_DEFAULT`] — как решила
    /// измеренная политика поколения.
    pub use_entities_lower: u8,
    /// Корпусная таблица по точной форме.
    pub use_exact: u8,
    /// Корпусная таблица в нижнем регистре.
    pub use_lower: u8,
    /// Повтор точной корпусной таблицы по ключу в нижнем регистре.
    pub use_exact_lower: u8,
    /// Внешний словарь-анализатор.
    pub use_external: u8,
    /// Он же по ключу в нижнем регистре.
    pub use_external_lower: u8,
    /// Суффиксные правила для незнакомых форм.
    pub use_rules: u8,
    /// Не искать вовсе, возвращать поверхность.
    pub protected: u8,
}

/// `use_entities_lower`: взять решение измеренной политики поколения.
pub const UDLEX_PROBE_DEFAULT: u8 = 2;

impl From<UdlexOptions> for Options {
    fn from(value: UdlexOptions) -> Self {
        Self {
            use_entities: value.use_entities != 0,
            use_entities_lower: !matches!(value.use_entities_lower, 0 | UDLEX_PROBE_DEFAULT),
            use_exact: value.use_exact != 0,
            use_lower: value.use_lower != 0,
            use_exact_lower: value.use_exact_lower != 0,
            use_external: value.use_external != 0,
            use_external_lower: value.use_external_lower != 0,
            use_rules: value.use_rules != 0,
            // The legacy open/load ABI is deliberately base-only. A user
            // layer can only be attached by the explicit layered entrypoints.
            use_user: false,
            protected: value.protected != 0,
        }
    }
}

/// Открытый словарь. Непрозрачен для C: раскладка структуры — не контракт.
#[derive(Debug)]
pub struct UdlexLexicon {
    inner: RuntimeLexicon,
    options: Options,
    /// Строчный ретрай спанового пути: только он может следовать измеренной
    /// политике поколения; узкий entity-профиль токена остаётся явным выбором.
    span_lower: bool,
}

// --- Разбор аргументов ---------------------------------------------------

/// Ошибка, у которой уже есть код возврата и текст для вызывающего.
struct Failure(i32, String);

type Outcome = Result<i32, Failure>;

/// Обязательный выходной указатель вызывающего.
fn require<T>(ptr: *mut T, what: &str) -> Result<(), Failure> {
    if ptr.is_null() {
        return Err(Failure(UDLEX_ERR_ARG, format!("{what}: нулевой указатель")));
    }
    Ok(())
}

/// Читает строку вызывающего как `&str`.
///
/// # Safety
///
/// `ptr` либо null при `len == 0`, либо валидный указатель на `len` байт,
/// живущих до конца вызова.
unsafe fn borrow_str<'a>(ptr: *const u8, len: usize, what: &str) -> Result<&'a str, Failure> {
    if len == 0 {
        return Ok("");
    }
    if ptr.is_null() {
        return Err(Failure(UDLEX_ERR_ARG, format!("{what}: нулевой указатель")));
    }
    let bytes = unsafe { std::slice::from_raw_parts(ptr, len) };
    std::str::from_utf8(bytes)
        .map_err(|error| Failure(UDLEX_ERR_UTF8, format!("{what}: не UTF-8: {error}")))
}

/// Разбирает пакетный вход: непрерывные байты плюс `count + 1` смещений. UTF-8
/// проверяется один раз на весь буфер, границы — `str::get`, который не режет
/// символ.
///
/// # Safety
///
/// Указатели описывают живые до конца вызова буферы заявленных длин.
unsafe fn borrow_tokens<'a>(
    data: *const u8,
    data_len: usize,
    offsets: *const u32,
    count: usize,
    what: &str,
) -> Result<Vec<&'a str>, Failure> {
    if count == 0 {
        return Ok(Vec::new());
    }
    if offsets.is_null() {
        return Err(Failure(
            UDLEX_ERR_ARG,
            format!("{what}: нулевой массив смещений"),
        ));
    }
    let text = unsafe { borrow_str(data, data_len, what) }?;
    let Some(bounds) = count.checked_add(1) else {
        return Err(Failure(
            UDLEX_ERR_ARG,
            format!("{what}: переполнение счёта"),
        ));
    };
    let offsets = unsafe { std::slice::from_raw_parts(offsets, bounds) };
    let mut tokens = Vec::with_capacity(count);
    for index in 0..count {
        let (Some(&start), Some(&end)) = (offsets.get(index), offsets.get(index.saturating_add(1)))
        else {
            return Err(Failure(UDLEX_ERR_ARG, format!("{what}: смещение за краем")));
        };
        let (start, end) = (start as usize, end as usize);
        if start > end {
            return Err(Failure(
                UDLEX_ERR_ARG,
                format!("{what}: смещения не возрастают на токене {index}"),
            ));
        }
        let Some(token) = text.get(start..end) else {
            return Err(Failure(
                UDLEX_ERR_UTF8,
                format!("{what}: токен {index} режет символ или выходит за буфер"),
            ));
        };
        tokens.push(token);
    }
    Ok(tokens)
}

/// Необязательная маска по одному байту на токен.
///
/// # Safety
///
/// `ptr` либо null, либо валиден на `count` байт.
unsafe fn borrow_flags<'a>(ptr: *const u8, count: usize) -> Option<&'a [u8]> {
    if ptr.is_null() || count == 0 {
        return None;
    }
    Some(unsafe { std::slice::from_raw_parts(ptr, count) })
}

/// Приёмник ответов: пишет, пока хватает места, и всегда считает нужный размер,
/// чтобы повтор после переполнения был ровно один.
struct Sink {
    ptr: *mut u8,
    cap: usize,
    written: usize,
    overflow: bool,
}

impl Sink {
    unsafe fn new(ptr: *mut u8, cap: usize) -> Self {
        Self {
            ptr,
            cap: if ptr.is_null() { 0 } else { cap },
            written: 0,
            overflow: false,
        }
    }

    fn push(&mut self, bytes: &[u8]) {
        let Some(end) = self.written.checked_add(bytes.len()) else {
            self.overflow = true;
            return;
        };
        if end > self.cap {
            self.overflow = true;
            self.written = end;
            return;
        }
        if !bytes.is_empty() {
            // Вход и выход не пересекаются — контракт заголовка.
            unsafe {
                std::ptr::copy_nonoverlapping(
                    bytes.as_ptr(),
                    self.ptr.add(self.written),
                    bytes.len(),
                );
            };
        }
        self.written = end;
    }

    fn status(&self) -> i32 {
        if self.overflow {
            UDLEX_MORE_SPACE
        } else {
            UDLEX_OK
        }
    }
}

// --- Обёртка границы -----------------------------------------------------

/// Единственная точка, где паника не уходит в чужой рантайм.
unsafe fn boundary<F: FnOnce() -> Outcome>(err: *mut UdlexBytes, body: F) -> i32 {
    if !err.is_null() {
        unsafe { std::ptr::write(err, UdlexBytes::EMPTY) };
    }
    let outcome = catch_unwind(AssertUnwindSafe(body)).unwrap_or_else(|_| {
        Err(Failure(
            UDLEX_ERR_PANIC,
            "паника внутри udlex, перехвачена на границе C".to_owned(),
        ))
    });
    match outcome {
        Ok(status) => status,
        Err(Failure(code, message)) => {
            if !err.is_null() {
                unsafe { std::ptr::write(err, UdlexBytes::owned(message)) };
            }
            code
        }
    }
}

fn open_failure(error: &udlex_rs::Error) -> Failure {
    Failure(UDLEX_ERR_OPEN, error.to_string())
}

fn entities_failure(error: &udlex_rs::Error) -> Failure {
    Failure(UDLEX_ERR_ENTITIES, error.to_string())
}

/// Открывает таблицу сущностей, если вызов до неё дойдёт, — там же, где
/// Python-биндинг.
fn ensure_entities(lexicon: &UdlexLexicon, needed: bool) -> Result<(), Failure> {
    if !needed || lexicon.inner.entities_ready() {
        return Ok(());
    }
    lexicon
        .inner
        .ensure_entities()
        .map_err(|e| entities_failure(&e))
}

/// Дойдёт ли потокенный вызов с таким профилем до таблицы сущностей.
const fn token_reaches_entities(options: Options) -> bool {
    !options.protected && (options.use_entities || options.use_entities_lower)
}

unsafe fn lexicon_ref<'a>(handle: *const UdlexLexicon) -> Result<&'a UdlexLexicon, Failure> {
    if handle.is_null() {
        return Err(Failure(
            UDLEX_ERR_ARG,
            "нулевой указатель словаря".to_owned(),
        ));
    }
    Ok(unsafe { &*handle })
}

fn call_options(base: Options, flags: u8) -> Options {
    base.for_call(flags & UDLEX_PROTECTED != 0, flags & UDLEX_ENTITY != 0)
}

/// Отдаёт один ответ вызывающему. Ответ, равный `input` — строке вызывающего,
/// — уходит как [`UDLEX_SAME`] без копии; `None` — поверхность склеена здесь.
unsafe fn deliver(
    text: &str,
    input: Option<&str>,
    source: Source,
    out: *mut u8,
    cap: usize,
    out_len: *mut usize,
    out_source: *mut u8,
) -> i32 {
    if !out_source.is_null() {
        unsafe { std::ptr::write(out_source, source.code()) };
    }
    if !out_len.is_null() {
        unsafe { std::ptr::write(out_len, text.len()) };
    }
    if input == Some(text) {
        return UDLEX_SAME;
    }
    if text.is_empty() {
        // Иначе вызывающий растил бы буфер до нуля и звал снова.
        return UDLEX_OK;
    }
    if text.len() > cap || out.is_null() {
        return UDLEX_MORE_SPACE;
    }
    let mut sink = unsafe { Sink::new(out, cap) };
    sink.push(text.as_bytes());
    sink.status()
}

// --- Открытие и закрытие -------------------------------------------------

/// Версия крейта как строка с завершающим нулём.
#[unsafe(no_mangle)]
pub extern "C" fn udlex_version() -> *const std::ffi::c_char {
    concat!(env!("CARGO_PKG_VERSION"), "\0")
        .as_ptr()
        .cast::<std::ffi::c_char>()
}

/// Освобождает буфер, выданный библиотекой.
///
/// # Safety
///
/// `bytes` получен из этой библиотеки и ещё не освобождался.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_bytes_free(bytes: UdlexBytes) {
    if bytes.ptr.is_null() || bytes.len == 0 {
        return;
    }
    let slice = std::ptr::slice_from_raw_parts_mut(bytes.ptr, bytes.len);
    drop(unsafe { Box::from_raw(slice) });
}

/// Открывает скомпилированный каталог словаря.
///
/// `span_case` и `span_multiword` — необязательные переопределения измеренных
/// конвенций спанов; `NULL` оставляет то, что несёт поколение.
///
/// # Safety
///
/// Указатели валидны на заявленные длины; `out` — на один указатель.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_open(
    path: *const u8,
    path_len: usize,
    options: *const UdlexOptions,
    span_case: *const u8,
    span_case_len: usize,
    span_multiword: *const u8,
    span_multiword_len: usize,
    out: *mut *mut UdlexLexicon,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        open_with(
            options,
            (span_case, span_case_len),
            (span_multiword, span_multiword_len),
            out,
            err,
            |_| {
                let path = borrow_str(path, path_len, "path")?;
                let base = Lexicon::open(Path::new(path)).map_err(|e| open_failure(&e))?;
                Ok(RuntimeLexicon::Base(Box::new(base)))
            },
        )
    }
}

/// Открывает словарь одного языка из бандла: `rus` или `ru_taiga`.
///
/// # Safety
///
/// Указатели валидны на заявленные длины; `out` — на один указатель.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_load(
    bundle: *const u8,
    bundle_len: usize,
    language: *const u8,
    language_len: usize,
    options: *const UdlexOptions,
    span_case: *const u8,
    span_case_len: usize,
    span_multiword: *const u8,
    span_multiword_len: usize,
    out: *mut *mut UdlexLexicon,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        open_with(
            options,
            (span_case, span_case_len),
            (span_multiword, span_multiword_len),
            out,
            err,
            |_| {
                let bundle = borrow_str(bundle, bundle_len, "bundle")?;
                let language = borrow_str(language, language_len, "language")?;
                let base =
                    udlex_rs::load(Path::new(bundle), language).map_err(|e| open_failure(&e))?;
                Ok(RuntimeLexicon::Base(Box::new(base)))
            },
        )
    }
}

/// Открывает базовый каталог и явно подключает независимый user-бандл.
///
/// Валидный user root без каталога ISO даёт base-only объект. Отсутствующий
/// или повреждённый явно переданный root всегда является ошибкой, даже при
/// `use_user == 0`; этот флаг отключает lookup, а не проверку конфигурации.
///
/// # Safety
///
/// Указатели валидны на заявленные длины; `out` — на один указатель.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_open_layered(
    path: *const u8,
    path_len: usize,
    user_bundle: *const u8,
    user_bundle_len: usize,
    use_user: u8,
    options: *const UdlexOptions,
    span_case: *const u8,
    span_case_len: usize,
    span_multiword: *const u8,
    span_multiword_len: usize,
    out: *mut *mut UdlexLexicon,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        open_with(
            options,
            (span_case, span_case_len),
            (span_multiword, span_multiword_len),
            out,
            err,
            |options| {
                let path = borrow_str(path, path_len, "path")?;
                let user_bundle = borrow_str(user_bundle, user_bundle_len, "user_bundle")?;
                options.use_user = use_user != 0;
                let base = Lexicon::open(Path::new(path)).map_err(|e| open_failure(&e))?;
                attach_user(base, Path::new(user_bundle))
            },
        )
    }
}

/// Грузит язык из базового бандла и явно подключает независимый user-бандл.
///
/// # Safety
///
/// Указатели валидны на заявленные длины; `out` — на один указатель.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_load_layered(
    bundle: *const u8,
    bundle_len: usize,
    language: *const u8,
    language_len: usize,
    user_bundle: *const u8,
    user_bundle_len: usize,
    use_user: u8,
    options: *const UdlexOptions,
    span_case: *const u8,
    span_case_len: usize,
    span_multiword: *const u8,
    span_multiword_len: usize,
    out: *mut *mut UdlexLexicon,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        open_with(
            options,
            (span_case, span_case_len),
            (span_multiword, span_multiword_len),
            out,
            err,
            |options| {
                let bundle = borrow_str(bundle, bundle_len, "bundle")?;
                let language = borrow_str(language, language_len, "language")?;
                let user_bundle = borrow_str(user_bundle, user_bundle_len, "user_bundle")?;
                options.use_user = use_user != 0;
                let base =
                    udlex_rs::load(Path::new(bundle), language).map_err(|e| open_failure(&e))?;
                attach_user(base, Path::new(user_bundle))
            },
        )
    }
}

/// Общее тело четырёх функций открытия: профиль, словарь из `open`, конвенции
/// спанов и выдача указателя.
///
/// # Safety
///
/// Указатели валидны на заявленные длины; `out` — на один указатель.
unsafe fn open_with(
    options: *const UdlexOptions,
    span_case: (*const u8, usize),
    span_multiword: (*const u8, usize),
    out: *mut *mut UdlexLexicon,
    err: *mut UdlexBytes,
    open: impl FnOnce(&mut Options) -> Result<RuntimeLexicon, Failure>,
) -> i32 {
    unsafe {
        boundary(err, || {
            let (mut options, lower_by_generation) = read_options(options);
            let inner = open(&mut options)?;
            publish(
                inner,
                options,
                lower_by_generation,
                borrow_opt(span_case.0, span_case.1, "span_case")?,
                borrow_opt(span_multiword.0, span_multiword.1, "span_multiword")?,
                out,
            )
        })
    }
}

fn attach_user(base: Lexicon, user_bundle: &Path) -> Result<RuntimeLexicon, Failure> {
    let user = UserDictionary::open_from_bundle(user_bundle, &base.meta().iso639_3)
        .map_err(|error| open_failure(&error))?;
    RuntimeLexicon::new(base, user).map_err(|error| open_failure(&error))
}

/// Профиль из структуры вызывающего и флаг «строчный ретрай — по поколению».
unsafe fn read_options(options: *const UdlexOptions) -> (Options, bool) {
    if options.is_null() {
        (Options::default(), true)
    } else {
        let raw = unsafe { *options };
        (
            Options::from(raw),
            raw.use_entities_lower == UDLEX_PROBE_DEFAULT,
        )
    }
}

/// Необязательная строка: `NULL` означает «не задано», а не «пустая строка».
unsafe fn borrow_opt<'a>(
    ptr: *const u8,
    len: usize,
    what: &str,
) -> Result<Option<&'a str>, Failure> {
    if ptr.is_null() {
        return Ok(None);
    }
    unsafe { borrow_str(ptr, len, what) }.map(Some)
}

fn publish(
    mut inner: RuntimeLexicon,
    options: Options,
    lower_by_generation: bool,
    span_case: Option<&str>,
    span_multiword: Option<&str>,
    out: *mut *mut UdlexLexicon,
) -> Outcome {
    require(out, "out")?;
    apply_conventions(&mut inner, span_case, span_multiword)?;
    let span_lower = if lower_by_generation {
        inner.base().span_options().use_entities_lower
    } else {
        options.use_entities_lower
    };
    let lexicon = UdlexLexicon {
        inner,
        options,
        span_lower,
    };
    unsafe { std::ptr::write(out, Box::into_raw(Box::new(lexicon))) };
    Ok(UDLEX_OK)
}

/// Переопределяет измеренные конвенции спанов; нужно только приёмке, в
/// продакшене обе строки — `NULL`.
fn apply_conventions(
    inner: &mut RuntimeLexicon,
    span_case: Option<&str>,
    span_multiword: Option<&str>,
) -> Result<(), Failure> {
    if span_case.is_none() && span_multiword.is_none() {
        return Ok(());
    }
    let mut conventions = inner.span_conventions();
    if let Some(value) = span_case {
        let policy = udlex_rs::format::SpanCase::parse(value).ok_or_else(|| {
            Failure(
                UDLEX_ERR_ARG,
                format!("span_case: ожидалось \"transfer\" или \"verbatim\", получено {value:?}"),
            )
        })?;
        conventions.case_transfer = policy.transfers();
    }
    if let Some(value) = span_multiword {
        let policy = udlex_rs::format::SpanMultiword::parse(value).ok_or_else(|| {
            Failure(
                UDLEX_ERR_ARG,
                format!(
                    "span_multiword: ожидалось \"identity\", \"compose\" или \"head\", получено {value:?}"
                ),
            )
        })?;
        conventions.multiword = policy;
    }
    inner.set_span_conventions(conventions);
    Ok(())
}

/// Закрывает словарь. Повторный вызов на том же указателе — ошибка вызывающего.
///
/// # Safety
///
/// `lexicon` получен из [`udlex_open`] или [`udlex_load`] и больше не в работе.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_close(lexicon: *mut UdlexLexicon) {
    if lexicon.is_null() {
        return;
    }
    drop(unsafe { Box::from_raw(lexicon) });
}

/// Открывает и проверяет таблицу сущностей заранее.
///
/// # Safety
///
/// `lexicon` — живой указатель из этой библиотеки.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_ensure_entities(
    lexicon: *const UdlexLexicon,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            lexicon
                .inner
                .ensure_entities()
                .map_err(|e| entities_failure(&e))?;
            Ok(UDLEX_OK)
        })
    }
}

/// Открыта ли уже таблица сущностей: спановый запрос по ней ничего не читает.
///
/// # Safety
///
/// `lexicon` — живой указатель из этой библиотеки или `NULL`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_entities_ready(lexicon: *const UdlexLexicon) -> u8 {
    if lexicon.is_null() {
        return 0;
    }
    u8::from(unsafe { &*lexicon }.inner.entities_ready())
}

// --- Константы формата ---------------------------------------------------

/// Читает ли эта сборка бандл, объявивший такую версию раскладки.
#[unsafe(no_mangle)]
pub extern "C" fn udlex_format_supported(format_version: u32) -> u8 {
    u8::from(udlex_rs::FormatRange::READ.accepts(format_version))
}

/// Что делать вызывающему с форматом, который эта сборка не читает.
///
/// # Safety
///
/// `out` указывает на одну структуру `UdlexBytes`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_format_remedy(
    format_version: u32,
    out: *mut UdlexBytes,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            require(out, "out")?;
            let remedy = udlex_rs::FormatRange::READ.remedy(format_version);
            std::ptr::write(out, UdlexBytes::owned(remedy.to_owned()));
            Ok(UDLEX_OK)
        })
    }
}

#[derive(serde::Serialize)]
struct ConstantsView {
    version: &'static str,
    format_version: u32,
    min_format_version: u32,
    format_range: String,
    min_core_release: &'static str,
    entity_encoding: &'static str,
    normalization: &'static str,
    phrase_codec: &'static str,
    phrase_separator: String,
    phrase_tail: usize,
    sources: [&'static str; 6],
}

/// Константы формата одним JSON-объектом — тот же набор, что атрибуты модуля
/// Python.
///
/// # Safety
///
/// `out` указывает на одну структуру `UdlexBytes`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_constants_json(out: *mut UdlexBytes, err: *mut UdlexBytes) -> i32 {
    unsafe {
        boundary(err, || {
            let view = ConstantsView {
                version: env!("CARGO_PKG_VERSION"),
                format_version: udlex_rs::FORMAT_VERSION,
                min_format_version: udlex_rs::MIN_FORMAT_VERSION,
                format_range: udlex_rs::FormatRange::READ.to_string(),
                min_core_release: udlex_rs::format::MIN_CORE_RELEASE,
                entity_encoding: udlex_rs::ENTITY_ENCODING,
                normalization: udlex_rs::NORMALIZATION,
                phrase_codec: udlex_rs::PHRASE_CODEC,
                phrase_separator: udlex_rs::PHRASE_SEPARATOR.to_string(),
                phrase_tail: udlex_rs::PHRASE_TAIL,
                sources: udlex_rs::SOURCES,
            };
            emit_json(&view, out)
        })
    }
}

// --- Метаданные ----------------------------------------------------------

#[derive(serde::Serialize)]
struct MetaView<'a> {
    name: &'a str,
    language: &'a str,
    iso639_3: &'a str,
    source: &'a str,
    variant: &'a str,
    selected_on: Option<&'a str>,
    training_tokens: u64,
    entries: u64,
    accuracy: Option<f64>,
    baseline: Option<f64>,
    is_useful: Option<bool>,
    base_accuracy: Option<f64>,
    base_baseline: Option<f64>,
    base_is_useful: Option<bool>,
    has_user_dictionary: bool,
    user_entries: u64,
    user_generation: Option<&'a str>,
    generation: Option<&'a str>,
    format_version: u32,
    normalization: &'a str,
    span_case: Option<&'a str>,
    span_multiword: Option<&'a str>,
    // Действующие конвенции: отличаются от поколения только после override.
    effective_span_case: &'static str,
    effective_span_multiword: &'static str,
    entity_lower: bool,
    effective_entities_lower: bool,
}

#[derive(serde::Serialize)]
struct LanguageView {
    iso639_3: String,
    name: String,
    lexicon: String,
    variant: String,
    training_tokens: u64,
    entries: u64,
    accuracy: Option<f64>,
    baseline: Option<f64>,
    is_useful: Option<bool>,
    selected_on: Option<String>,
    selection_accuracy: Option<f64>,
    selection_baseline: Option<f64>,
}

fn emit_json(value: &impl serde::Serialize, out: *mut UdlexBytes) -> Outcome {
    require(out, "out")?;
    let text = serde_json::to_string(value)
        .map_err(|error| Failure(UDLEX_ERR_ARG, format!("сериализация: {error}")))?;
    unsafe { std::ptr::write(out, UdlexBytes::owned(text)) };
    Ok(UDLEX_OK)
}

/// Метаданные открытого словаря одним JSON-объектом.
///
/// # Safety
///
/// `lexicon` живой, `out` — на одну структуру `UdlexBytes`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_meta_json(
    lexicon: *const UdlexLexicon,
    out: *mut UdlexBytes,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let meta = lexicon.inner.meta();
            let conventions = lexicon.inner.span_conventions();
            // Те же четыре таблицы, что считает `udlex_languages_json`.
            let entries = ["exact", "lower", "external", "entities"]
                .into_iter()
                .filter_map(|table| meta.entries.get(table).copied())
                .fold(0u64, u64::saturating_add);
            let view = MetaView {
                name: &meta.name,
                language: &meta.language,
                iso639_3: &meta.iso639_3,
                source: &meta.source,
                variant: &meta.variant,
                selected_on: meta.selected_on.as_deref(),
                training_tokens: meta.training_tokens,
                entries,
                accuracy: (!lexicon.inner.has_user_dictionary())
                    .then_some(meta.accuracy)
                    .flatten(),
                baseline: (!lexicon.inner.has_user_dictionary())
                    .then_some(meta.baseline)
                    .flatten(),
                is_useful: lexicon.inner.composite_is_useful(),
                base_accuracy: meta.accuracy,
                base_baseline: meta.baseline,
                base_is_useful: lexicon.inner.base().is_useful(),
                has_user_dictionary: lexicon.inner.has_user_dictionary(),
                user_entries: lexicon.inner.user_entries(),
                user_generation: lexicon.inner.user_generation(),
                generation: meta.generation.as_deref(),
                format_version: meta.format_version,
                normalization: &meta.normalization,
                span_case: meta.span_case.as_deref(),
                span_multiword: meta.span_multiword.as_deref(),
                effective_span_case: if conventions.case_transfer {
                    udlex_rs::format::SpanCase::Transfer.name()
                } else {
                    udlex_rs::format::SpanCase::Verbatim.name()
                },
                effective_span_multiword: conventions.multiword.name(),
                entity_lower: meta.entity_lower,
                effective_entities_lower: lexicon.span_lower,
            };
            emit_json(&view, out)
        })
    }
}

/// Каталог бандла одним JSON-массивом.
///
/// # Safety
///
/// Указатели валидны на заявленные длины.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_languages_json(
    bundle: *const u8,
    bundle_len: usize,
    out: *mut UdlexBytes,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let bundle = borrow_str(bundle, bundle_len, "bundle")?;
            let languages = udlex_rs::languages(Path::new(bundle)).map_err(|e| open_failure(&e))?;
            let view: Vec<LanguageView> = languages
                .into_iter()
                .map(|language| LanguageView {
                    iso639_3: language.iso639_3,
                    name: language.name,
                    lexicon: language.lexicon,
                    variant: language.variant,
                    training_tokens: language.training_tokens,
                    entries: language.entries,
                    accuracy: language.accuracy,
                    baseline: language.baseline,
                    is_useful: language.is_useful,
                    selected_on: language.selected_on,
                    selection_accuracy: language.selection_accuracy,
                    selection_baseline: language.selection_baseline,
                })
                .collect();
            emit_json(&view, out)
        })
    }
}

// --- Потокенный путь -----------------------------------------------------

/// Лемма одного токена.
///
/// # Safety
///
/// Указатели валидны на заявленные длины и не пересекаются с `out`.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_lemma(
    lexicon: *const UdlexLexicon,
    token: *const u8,
    token_len: usize,
    flags: u8,
    out: *mut u8,
    out_cap: usize,
    out_len: *mut usize,
    out_source: *mut u8,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let token = borrow_str(token, token_len, "token")?;
            let options = call_options(lexicon.options, flags);
            ensure_entities(lexicon, token_reaches_entities(options))?;
            let answer =
                lexicon
                    .inner
                    .lemma_with_info(token, flags & UDLEX_SENTENCE_INITIAL != 0, options);
            Ok(deliver(
                &answer.lemma,
                Some(token),
                answer.source,
                out,
                out_cap,
                out_len,
                out_source,
            ))
        })
    }
}

/// Отвечает ли хоть одна таблица на эту форму, без суффиксных правил.
///
/// # Safety
///
/// Указатели валидны на заявленные длины.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn udlex_knows(
    lexicon: *const UdlexLexicon,
    token: *const u8,
    token_len: usize,
    flags: u8,
    out: *mut u8,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let token = borrow_str(token, token_len, "token")?;
            let options = call_options(lexicon.options, flags);
            ensure_entities(lexicon, token_reaches_entities(options))?;
            require(out, "out")?;
            let known =
                lexicon
                    .inner
                    .knows_with(token, flags & UDLEX_SENTENCE_INITIAL != 0, options);
            std::ptr::write(out, u8::from(known));
            Ok(UDLEX_OK)
        })
    }
}

/// Лемматизирует пакет токенов за один переход границы.
///
/// `flags` — либо `NULL` (первый токен начинает предложение, масок нет), либо
/// `count` байт из [`UDLEX_SENTENCE_INITIAL`], [`UDLEX_PROTECTED`] и
/// [`UDLEX_ENTITY`]. `out_offsets` обязателен и вмещает `count + 1` элемент.
/// `out_same` обязателен и содержит отдельный транспортный флаг равенства;
/// `out_sources` опционален и содержит только коды [`Source`].
///
/// # Safety
///
/// Все указатели валидны на заявленные длины; входные и выходные буферы не
/// пересекаются.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_lemmatize(
    lexicon: *const UdlexLexicon,
    data: *const u8,
    data_len: usize,
    offsets: *const u32,
    count: usize,
    flags: *const u8,
    out_data: *mut u8,
    out_cap: usize,
    out_offsets: *mut u32,
    out_same: *mut u8,
    out_sources: *mut u8,
    out_len: *mut usize,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let tokens = borrow_tokens(data, data_len, offsets, count, "tokens")?;
            let out = (
                out_data,
                out_cap,
                out_offsets,
                out_same,
                out_sources,
                out_len,
            );
            lemmatize_batch(lexicon, &tokens, flags, out, |_, token, flag, options| {
                lexicon
                    .inner
                    .lemma_with_info(token, flag & UDLEX_SENTENCE_INITIAL != 0, options)
            })
        })
    }
}

/// [`udlex_lemmatize`] с тем, что вызывающий знает о предложении.
///
/// `upos_offsets` — `NULL` или `count + 1` смещений тегов UPOS в `upos_data`,
/// по тегу на токен (пустой тег не спрашивается); `neighbours != 0` спрашивает
/// ключи соседних слов, и граница предложения из `flags` для них — край.
/// Точный ответ уступает ключу контекста, выученному сборщиком для
/// неоднозначной формы, с источником `context`.
///
/// # Safety
///
/// Как у [`udlex_lemmatize`]; буфер тегов живёт до конца вызова.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_lemmatize_context(
    lexicon: *const UdlexLexicon,
    data: *const u8,
    data_len: usize,
    offsets: *const u32,
    count: usize,
    flags: *const u8,
    upos_data: *const u8,
    upos_len: usize,
    upos_offsets: *const u32,
    neighbours: u8,
    out_data: *mut u8,
    out_cap: usize,
    out_offsets: *mut u32,
    out_same: *mut u8,
    out_sources: *mut u8,
    out_len: *mut usize,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let tokens = borrow_tokens(data, data_len, offsets, count, "tokens")?;
            let upos = if upos_offsets.is_null() {
                None
            } else {
                Some(borrow_tokens(
                    upos_data,
                    upos_len,
                    upos_offsets,
                    count,
                    "upos",
                )?)
            };
            let masks = borrow_flags(flags, count);
            let opens = |index| flag_at(masks, index) & UDLEX_SENTENCE_INITIAL != 0;
            let out = (
                out_data,
                out_cap,
                out_offsets,
                out_same,
                out_sources,
                out_len,
            );
            lemmatize_batch(
                lexicon,
                &tokens,
                flags,
                out,
                |index, token, flag, options| {
                    let (previous, next) = udlex_rs::format::neighbours(&tokens, index, opens);
                    let wanted = neighbours != 0;
                    lexicon.inner.lemma_in_context(
                        token,
                        flag & UDLEX_SENTENCE_INITIAL != 0,
                        options,
                        upos.as_ref().and_then(|tags| tags.get(index).copied()),
                        wanted.then_some(previous),
                        wanted.then_some(next),
                    )
                },
            )
        })
    }
}

/// Флаги токена: из маски, а без неё первый токен открывает предложение.
fn flag_at(masks: Option<&[u8]>, index: usize) -> u8 {
    masks
        .and_then(|mask| mask.get(index).copied())
        .unwrap_or(if index == 0 {
            UDLEX_SENTENCE_INITIAL
        } else {
            0
        })
}

/// Выходные буферы пакета: данные, ёмкость, смещения, `same`, источники, длина.
type BatchOut = (*mut u8, usize, *mut u32, *mut u8, *mut u8, *mut usize);

/// Общее тело пакетных вызовов: проверка выхода, таблица сущностей, раскладка
/// ответов. Ответ на токен даёт `answer(index, token, flags, options)`.
///
/// # Safety
///
/// Как у [`udlex_lemmatize`].
unsafe fn lemmatize_batch<'a>(
    lexicon: &'a UdlexLexicon,
    tokens: &[&'a str],
    flags: *const u8,
    (out_data, out_cap, out_offsets, out_same, out_sources, out_len): BatchOut,
    answer: impl Fn(usize, &'a str, u8, Options) -> udlex_rs::Answer<'a>,
) -> Outcome {
    unsafe {
        let count = tokens.len();
        if count == 0 {
            if !out_len.is_null() {
                std::ptr::write(out_len, 0);
            }
            if !out_offsets.is_null() {
                std::ptr::write(out_offsets, 0);
            }
            return Ok(UDLEX_OK);
        }
        require(out_offsets, "out_offsets")?;
        require(out_same, "out_same")?;
        let masks = borrow_flags(flags, count);
        // Таблица сущностей (у ru — десятки МБ) открывается, только если до неё
        // дойдёт хоть одна строка; зеркало питоновской batch_needs_entities.
        let reaches = |flags: u8| {
            let call = call_options(lexicon.options, flags);
            call.use_entities && !call.protected
        };
        let needed = masks.map_or_else(|| reaches(0), |flags| flags.iter().any(|&f| reaches(f)));
        ensure_entities(lexicon, needed)?;
        let bounds = count
            .checked_add(1)
            .ok_or_else(|| Failure(UDLEX_ERR_ARG, "переполнение счёта токенов".to_owned()))?;
        let out_offsets = std::slice::from_raw_parts_mut(out_offsets, bounds);
        let out_same = std::slice::from_raw_parts_mut(out_same, count);
        let mut sources = if out_sources.is_null() {
            None
        } else {
            Some(std::slice::from_raw_parts_mut(out_sources, count))
        };
        let mut sink = Sink::new(out_data, out_cap);
        if let Some(slot) = out_offsets.first_mut() {
            *slot = 0;
        }
        for (index, token) in tokens.iter().enumerate() {
            let flag = flag_at(masks, index);
            let options = call_options(lexicon.options, flag);
            let answer = answer(index, token, flag, options);
            let same = answer.lemma == *token;
            if let Some(slot) = out_same.get_mut(index) {
                *slot = u8::from(same);
            }
            if !same {
                sink.push(answer.lemma.as_bytes());
            }
            if let Some(slot) = out_offsets.get_mut(index.saturating_add(1)) {
                *slot = u32::try_from(sink.written).unwrap_or(u32::MAX);
            }
            if let Some(slot) = sources.as_mut().and_then(|sources| sources.get_mut(index)) {
                *slot = answer.source.code();
            }
        }
        if !out_len.is_null() {
            std::ptr::write(out_len, sink.written);
        }
        Ok(sink.status())
    }
}

// --- Спановый путь -------------------------------------------------------

/// Каноническая форма упоминания, пришедшего одной строкой.
///
/// # Safety
///
/// Указатели валидны на заявленные длины и не пересекаются с `out`.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_lemma_span_text(
    lexicon: *const UdlexLexicon,
    text: *const u8,
    text_len: usize,
    flags: u8,
    out: *mut u8,
    out_cap: usize,
    out_len: *mut usize,
    out_source: *mut u8,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let text = borrow_str(text, text_len, "text")?;
            let mut options = lexicon.options;
            options.use_entities_lower = lexicon.span_lower;
            options.protected |= flags & UDLEX_PROTECTED != 0;
            // Как в Python: ломаная поверхность отказывает раньше entity-таблицы.
            let (tokens, gaps) = udlex_rs::split_surface(text)
                .map_err(|error| Failure(UDLEX_ERR_SURFACE, error.to_string()))?;
            ensure_entities(lexicon, !options.protected)?;
            let separators = (!gaps.is_empty()).then_some(gaps.as_slice());
            let answer = lexicon.inner.lemma_span_with_info(
                text,
                &tokens,
                separators,
                flags & UDLEX_SENTENCE_INITIAL != 0,
                options,
            );
            Ok(deliver(
                &answer.lemma,
                Some(text),
                answer.source,
                out,
                out_cap,
                out_len,
                out_source,
            ))
        })
    }
}

/// Каноническая форма упоминания, заданного токенами и зазорами между ними.
///
/// Поверхность склеивается здесь — по тем же правилам, что в Python-биндинге:
/// без `separators` каждый стык становится одним пробелом.
///
/// # Safety
///
/// Все указатели валидны на заявленные длины; входные и выходные буферы не
/// пересекаются.
#[unsafe(no_mangle)]
#[allow(clippy::too_many_arguments)]
pub unsafe extern "C" fn udlex_lemma_span(
    lexicon: *const UdlexLexicon,
    data: *const u8,
    data_len: usize,
    offsets: *const u32,
    count: usize,
    sep_data: *const u8,
    sep_data_len: usize,
    sep_offsets: *const u32,
    sep_count: usize,
    has_separators: u8,
    flags: u8,
    out: *mut u8,
    out_cap: usize,
    out_len: *mut usize,
    out_source: *mut u8,
    err: *mut UdlexBytes,
) -> i32 {
    unsafe {
        boundary(err, || {
            let lexicon = lexicon_ref(lexicon)?;
            let tokens = borrow_tokens(data, data_len, offsets, count, "tokens")?;
            if tokens.is_empty() {
                return Err(Failure(
                    UDLEX_ERR_ARG,
                    "спан пуст: нужен хотя бы один токен".to_owned(),
                ));
            }
            let separators = if has_separators == 0 {
                None
            } else {
                Some(borrow_tokens(
                    sep_data,
                    sep_data_len,
                    sep_offsets,
                    sep_count,
                    "separators",
                )?)
            };
            let gaps = tokens.len().saturating_sub(1);
            if let Some(separators) = separators.as_ref()
                && separators.len() != gaps
            {
                return Err(Failure(
                    UDLEX_ERR_ARG,
                    format!(
                        "separators: {} элементов, ожидалось {gaps}",
                        separators.len()
                    ),
                ));
            }
            let surface = join_surface(&tokens, separators.as_deref());
            if surface.is_empty() {
                return Err(Failure(
                    UDLEX_ERR_SURFACE,
                    "поверхность спана пуста".to_owned(),
                ));
            }
            let mut options = lexicon.options;
            options.use_entities_lower = lexicon.span_lower;
            options.protected |= flags & UDLEX_PROTECTED != 0;
            ensure_entities(lexicon, !options.protected)?;
            let sentence_initial = flags & UDLEX_SENTENCE_INITIAL != 0;
            // Однотокенный спан — поверхность без списка границ, как в Python.
            let answer = if tokens.len() <= 1 {
                lexicon
                    .inner
                    .lemma_span_with_info(&surface, &[], None, sentence_initial, options)
            } else {
                lexicon.inner.lemma_span_with_info(
                    &surface,
                    &tokens,
                    separators.as_deref(),
                    sentence_initial,
                    options,
                )
            };
            Ok(deliver(
                &answer.lemma,
                None,
                answer.source,
                out,
                out_cap,
                out_len,
                out_source,
            ))
        })
    }
}

fn join_surface(tokens: &[&str], separators: Option<&[&str]>) -> String {
    let width = tokens
        .iter()
        .map(|token| token.len())
        .fold(0usize, usize::saturating_add)
        .saturating_add(separators.map_or_else(
            || tokens.len().saturating_sub(1),
            |gaps| {
                gaps.iter()
                    .map(|gap| gap.len())
                    .fold(0usize, usize::saturating_add)
            },
        ));
    let mut surface = String::with_capacity(width);
    for (index, token) in tokens.iter().enumerate() {
        if index > 0 {
            match separators.and_then(|gaps| gaps.get(index.saturating_sub(1))) {
                Some(gap) => surface.push_str(gap),
                None => surface.push(' '),
            }
        }
        surface.push_str(token);
    }
    surface
}
