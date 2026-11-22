package udlex

/*
#include "udlex.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"runtime"
	"strconv"
	"sync"
)

// ErrClosed возвращается вызовом к словарю, который уже закрыт.
var ErrClosed = errors.New("словарь закрыт")

// Option — настройка загрузки словаря; действует на весь объект, а не на вызов.
type Option func(*config)

type config struct {
	probes        Probes
	lowerChosen   bool
	spanCase      string
	spanMultiword string
	userBundle    string
	hasUserBundle bool
	useUser       bool
}

// WithUserDictionary явно подключает независимый user-бандл. Отсутствующий или
// повреждённый путь — ошибка загрузки; из окружения путь не читается.
func WithUserDictionary(path string) Option {
	return func(c *config) {
		c.userBundle = path
		c.hasUserBundle = true
	}
}

// WithoutUserDictionary оставляет подключённый user-бандл проверенным и видимым
// в [Meta], но выключает поиск по нему — для диагностики.
func WithoutUserDictionary() Option { return func(c *config) { c.useUser = false } }

// WithProbes задаёт профиль проб целиком.
//
// Deprecated: нулевое значение Probes выключает все таблицы разом. Используйте
// [WithoutRules], [WithoutExternal], [WithEntities].
func WithProbes(probes Probes) Option {
	return func(c *config) { c.probes, c.lowerChosen = probes, true }
}

// WithoutRules запрещает угадывать по суффиксу: незнакомая форма вернётся как есть.
func WithoutRules() Option { return func(c *config) { c.probes.UseRules = false } }

// WithoutExternal отключает внешний словарь-анализатор, оставляя корпусные таблицы.
func WithoutExternal() Option {
	return func(c *config) { c.probes.UseExternal, c.probes.UseExternalLower = false, false }
}

// WithEntities разрешает точную таблицу имён собственных на каждом вызове.
// Для отдельных токенов хватает [Call].Entity.
func WithEntities() Option { return func(c *config) { c.probes.UseEntities = true } }

// WithEntitiesLower добавляет повтор таблицы имён по ключу в нижнем регистре.
// Где его открыла измеренная политика (ces, rus), спановый путь делает это и
// так; опция включает повтор всем и в entity-профиле токена.
func WithEntitiesLower() Option {
	return func(c *config) {
		c.probes.UseEntities, c.probes.UseEntitiesLower, c.lowerChosen = true, true, true
	}
}

// AlwaysProtected закрепляет режим «не трогать» за всем объектом: каждый вызов
// вернёт свою строку без единого поиска. Флаг вызова снять это не может.
func AlwaysProtected() Option { return func(c *config) { c.probes.Protected = true } }

// WithSpanCase переопределяет измеренную конвенцию регистра спанов:
// [SpanCaseTransfer] или [SpanCaseVerbatim].
//
// Deprecated: нужно только приёмке, которая живёт в Python.
func WithSpanCase(policy string) Option { return func(c *config) { c.spanCase = policy } }

// WithSpanMultiword переопределяет конвенцию многословных спанов:
// [SpanMultiwordIdentity], [SpanMultiwordCompose] или [SpanMultiwordHead].
//
// Deprecated: нужно только приёмке, которая живёт в Python.
func WithSpanMultiword(policy string) Option {
	return func(c *config) { c.spanMultiword = policy }
}

// Значения конвенций спанов, как их пишут метаданные поколения.
const (
	SpanCaseTransfer      = "transfer" // перенести регистр поверхности на ответ
	SpanCaseVerbatim      = "verbatim" // вернуть значение таблицы нетронутым
	SpanMultiwordIdentity = "identity" // непокрытый спан вернуть как есть
	SpanMultiwordCompose  = "compose"  // собрать спан из пословных ответов
	SpanMultiwordHead     = "head"     // ответить за голову, хвост оставить
)

// Lexicon — открытый словарь одного языка.
//
// Неизменяем и безопасен для любого числа горутин. Пользоваться только по
// указателю: внутри лежит пул буферов.
type Lexicon struct {
	handle *lexiconHandle
	probes Probes
	meta   Meta
	pool   sync.Pool
}

// lexiconHandle отделён от [Lexicon], чтобы уборка забытого Close не держала
// сам объект достижимым.
type lexiconHandle struct {
	ptr   *C.udlex_lexicon
	close func()
}

// Load открывает словарь одного языка из бандла; language — код ISO 639-3
// (`rus`) или имя лексикона (`ru_taiga`).
//
//	lex, err := udlex.Load("bundle", "rus")
func Load(bundle, language string, options ...Option) (*Lexicon, error) {
	return open("load", options, func(c *config, probes *C.udlex_options, out **C.udlex_lexicon, cerr *C.udlex_bytes) C.int32_t {
		if c.hasUserBundle {
			return C.udlex_load_layered(
				stringPtr(bundle), C.size_t(len(bundle)),
				stringPtr(language), C.size_t(len(language)),
				stringPtr(c.userBundle), C.size_t(len(c.userBundle)), cFlag(c.useUser), probes,
				stringPtr(c.spanCase), C.size_t(len(c.spanCase)),
				stringPtr(c.spanMultiword), C.size_t(len(c.spanMultiword)),
				out, cerr)
		}
		return C.udlex_load(
			stringPtr(bundle), C.size_t(len(bundle)),
			stringPtr(language), C.size_t(len(language)),
			probes,
			stringPtr(c.spanCase), C.size_t(len(c.spanCase)),
			stringPtr(c.spanMultiword), C.size_t(len(c.spanMultiword)),
			out, cerr)
	})
}

// Open открывает скомпилированный каталог словаря напрямую, минуя каталог
// бандла: путь к корню языка или к конкретному поколению.
func Open(path string, options ...Option) (*Lexicon, error) {
	return open("open", options, func(c *config, probes *C.udlex_options, out **C.udlex_lexicon, cerr *C.udlex_bytes) C.int32_t {
		if c.hasUserBundle {
			return C.udlex_open_layered(
				stringPtr(path), C.size_t(len(path)),
				stringPtr(c.userBundle), C.size_t(len(c.userBundle)), cFlag(c.useUser), probes,
				stringPtr(c.spanCase), C.size_t(len(c.spanCase)),
				stringPtr(c.spanMultiword), C.size_t(len(c.spanMultiword)),
				out, cerr)
		}
		return C.udlex_open(
			stringPtr(path), C.size_t(len(path)), probes,
			stringPtr(c.spanCase), C.size_t(len(c.spanCase)),
			stringPtr(c.spanMultiword), C.size_t(len(c.spanMultiword)),
			out, cerr)
	})
}

func open(
	op string, options []Option,
	call func(*config, *C.udlex_options, **C.udlex_lexicon, *C.udlex_bytes) C.int32_t,
) (*Lexicon, error) {
	settings := config{probes: DefaultProbes(), useUser: true}
	for _, option := range options {
		option(&settings)
	}
	probes := cOptions(settings.probes, settings.lowerChosen)
	var (
		ptr  *C.udlex_lexicon
		cerr C.udlex_bytes
	)
	if status := call(&settings, &probes, &ptr, &cerr); status != statusOK {
		return nil, fail(op, status, cerr)
	}

	handle := &lexiconHandle{ptr: ptr}
	handle.close = sync.OnceFunc(func() {
		C.udlex_close(handle.ptr)
		handle.ptr = nil
	})
	lexicon := &Lexicon{handle: handle, probes: settings.probes}
	runtime.AddCleanup(lexicon, func(h *lexiconHandle) { h.close() }, handle)

	meta, err := lexicon.readMeta()
	if err != nil {
		return nil, errors.Join(err, lexicon.Close())
	}
	lexicon.meta = meta
	// Профиль, спрашивающий таблицу имён на каждом токене, проверяет её сразу:
	// повреждение — отказ загрузки, а не отказ посреди текста.
	if !settings.probes.Protected && settings.probes.UseEntities {
		if err := lexicon.EnsureEntities(); err != nil {
			return nil, errors.Join(err, lexicon.Close())
		}
	}
	return lexicon, nil
}

// Close освобождает таблицы. Повторный вызов безвреден; звать параллельно с
// работающим вызовом нельзя.
func (l *Lexicon) Close() error {
	l.handle.close()
	return nil
}

// Meta — паспорт словаря: язык, вариант, из чего выучен, что измерено.
func (l *Lexicon) Meta() Meta { return l.meta }

// Probes — профиль проб, с которым словарь загружен.
func (l *Lexicon) Probes() Probes { return l.probes }

// EnsureEntities открывает и проверяет таблицу именованных сущностей.
//
// Обычно она читается лениво, при первом спановом или entity-вызове; позовите
// EnsureEntities на старте, чтобы узнать о повреждении сразу.
func (l *Lexicon) EnsureEntities() error {
	ptr, err := l.pointer("ensure_entities")
	if err != nil {
		return err
	}
	var cerr C.udlex_bytes
	status := C.udlex_ensure_entities(ptr, &cerr)
	runtime.KeepAlive(l)
	if status != statusOK {
		return fail("ensure_entities", status, cerr)
	}
	return nil
}

// EntitiesReady сообщает, открыта ли уже таблица имён собственных.
func (l *Lexicon) EntitiesReady() bool {
	ptr, err := l.pointer("entities_ready")
	if err != nil {
		return false
	}
	ready := C.udlex_entities_ready(ptr)
	runtime.KeepAlive(l)
	return ready != 0
}

func (l *Lexicon) readMeta() (Meta, error) {
	ptr, err := l.pointer("meta")
	if err != nil {
		return Meta{}, err
	}
	var out, cerr C.udlex_bytes
	status := C.udlex_meta_json(ptr, &out, &cerr)
	runtime.KeepAlive(l)
	if status != statusOK {
		return Meta{}, fail("meta", status, cerr)
	}
	var meta Meta
	if err := json.Unmarshal([]byte(take(out)), &meta); err != nil {
		return Meta{}, &Error{Op: "meta", Message: err.Error()}
	}
	return meta, nil
}

// pointer отдаёт указатель ядра или ErrClosed, названный операцией вызывающего.
func (l *Lexicon) pointer(op string) (*C.udlex_lexicon, error) {
	if l.handle.ptr == nil {
		return nil, &Error{Op: op, Code: CodeClosed, Err: ErrClosed}
	}
	return l.handle.ptr, nil
}

// one допускает одну структуру опций: вторая значила бы, что вызывающий ждёт
// их сложения, и молча её не проглотить.
func one[T any](op, what string, values []T) (T, error) {
	var zero T
	switch len(values) {
	case 0:
		return zero, nil
	case 1:
		return values[0], nil
	default:
		return zero, &Error{
			Op:      op,
			Code:    CodeArg,
			Message: "передано " + strconv.Itoa(len(values)) + " структур " + what + ", допустима одна",
		}
	}
}

// Lemma возвращает лемму одного токена.
//
//	lex.Lemma("котами")                              // кот
//	lex.Lemma("Мама", udlex.Call{SentenceInitial: true})
//
// Незнакомая форма возвращается как есть. Ошибка значит, что поиска не было:
// строка не UTF-8, словарь закрыт или повреждена таблица имён собственных.
// На потоковом тексте зовите [Lexicon.Lemmatize]: один переход границы на всё
// предложение.
func (l *Lexicon) Lemma(token string, call ...Call) (string, error) {
	info, err := l.LemmaWithInfo(token, call...)
	return info.Lemma, err
}

// LemmaWithInfo возвращает лемму вместе со ступенью, которая её выдала: только
// так различимы «словарь не знает форму» и «знает, и ответ совпал с ней».
func (l *Lexicon) LemmaWithInfo(token string, call ...Call) (Info, error) {
	options, err := one("lemma", "Call", call)
	if err != nil {
		return Info{}, err
	}
	ptr, err := l.pointer("lemma")
	if err != nil {
		return Info{}, err
	}
	buf := l.grab()
	defer l.pool.Put(buf)
	buf.out = grow(buf.out, len(token)+outputSlack)
	for range attempts {
		status := C.udlex_lemma(ptr, stringPtr(token), C.size_t(len(token)),
			C.uint8_t(options.flags()), bytePtr(buf.out), C.size_t(len(buf.out)),
			&buf.length, &buf.source, &buf.cerr)
		runtime.KeepAlive(l)
		if status != statusMoreSpace {
			return answer("lemma", token, status, buf)
		}
		buf.out = grow(buf.out, int(buf.length))
	}
	return Info{}, errShortBuffer("lemma")
}

// Knows отвечает, нашла ли форму хоть одна таблица, — без суффиксных правил.
// Сравнением лемм это не заменить: известное слово с леммой, равной форме,
// выглядит как незнакомое.
func (l *Lexicon) Knows(token string, call ...Call) (bool, error) {
	options, err := one("knows", "Call", call)
	if err != nil {
		return false, err
	}
	ptr, err := l.pointer("knows")
	if err != nil {
		return false, err
	}
	buf := l.grab()
	defer l.pool.Put(buf)
	status := C.udlex_knows(ptr, stringPtr(token), C.size_t(len(token)),
		C.uint8_t(options.flags()), &buf.single, &buf.cerr)
	runtime.KeepAlive(l)
	if status != statusOK {
		return false, fail("knows", status, buf.cerr)
	}
	return buf.single != 0, nil
}
