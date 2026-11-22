package udlex

import (
	"fmt"
	"strconv"
)

// Source — ступень, которая выдала ответ. Значения и имена [Source.String]
// совпадают с полем source в Python-API.
type Source uint8

// Стабильные коды C ABI.
const (
	SourceEntity        Source = iota // точная запись газеттира
	SourceEntityLower                 // она же по ключу в нижнем регистре
	SourceExact                       // корпусная таблица по точной форме
	SourceLower                       // корпусная таблица в нижнем регистре
	SourceExactLower                  // точная корпусная таблица по ключу в нижнем регистре
	SourceExternal                    // внешний словарь-анализатор
	SourceExternalLower               // он же по ключу в нижнем регистре
	SourceRule                        // продуктивное суффиксное правило
	SourceIdentity                    // не ответил никто, осталась поверхность
	SourceProtected                   // вызывающий запретил трогать строку
	SourceComposed                    // многословный спан, собранный пословно
	SourcePhrase                      // многословный спан по фразовому шаблону
	SourceUser                        // точный ручной override
	SourceUserLower                   // ручной override по locale-aware lowercase
	SourceContext                     // ключ контекста точной таблицы (UPOS или сосед)
)

//nolint:gochecknoglobals // таблица имён по коду Source
var sourceNames = [...]string{
	"entity", "entity_lower", "exact", "lower", "exact_lower", "external",
	"external_lower", "rule", "identity", "protected", "composed", "phrase",
	"user", "user_lower", "context",
}

// String возвращает стабильное имя ступени: по нему считают покрытие и пишут
// логи, поэтому имена не меняются.
func (s Source) String() string {
	if s.IsValid() {
		return sourceNames[s]
	}
	return "source(" + strconv.Itoa(int(s)) + ")"
}

// IsValid сообщает, входит ли значение в enum этой версии. Методы пакета
// возвращают только допустимые значения; проверка нужна Source, восстановленному
// из числа или внешнего формата.
func (s Source) IsValid() bool { return s <= SourceContext }

// IsKnown сообщает, пришёл ли ответ из сохранённой записи словаря, а не из
// догадки. Правило, поверхность и пословная сборка — не записи.
func (s Source) IsKnown() bool {
	switch s {
	case SourceEntity, SourceEntityLower, SourceExact, SourceLower,
		SourceExactLower, SourceExternal, SourceExternalLower, SourceUser,
		SourceUserLower, SourceContext:
		return true
	default:
		return false
	}
}

// IsLemmatized сообщает, дал ли ответ хоть один этап лемматизации. Это не
// сравнение леммы с входом: начальная форма может совпасть с ним побайтово.
// Ложь только для [SourceIdentity], [SourceProtected] и неизвестных кодов.
func (s Source) IsLemmatized() bool {
	return s.IsValid() && s != SourceIdentity && s != SourceProtected
}

// Info — лемма и ступень, которая её выдала. Производные флаги не хранятся:
// info.Source.IsKnown(), info.Source.IsLemmatized() и info.Lemma != token.
type Info struct {
	Lemma  string // текст леммы
	Source Source // финальная ступень, которая выдала ответ
}

// Call — флаги одного потокенного вызова; в Python это именованные аргументы
// с теми же именами. Нулевое значение — обычный токен в середине предложения.
type Call struct {
	// SentenceInitial: токен открывает предложение. Важно только для слова с
	// заглавной — отделяет обычное слово от имени собственного.
	SentenceInitial bool
	// Protected возвращает строку как есть, без единого поиска.
	Protected bool
	// Entity включает узкий entity-профиль: точная таблица имён собственных,
	// затем сам текст. Спановый путь шире, см. [Lexicon.LemmaSpanText].
	Entity bool
}

func (c Call) flags() uint8 {
	return flagIf(c.SentenceInitial, flagSentenceInitial) | flagIf(c.Protected, flagProtected) |
		flagIf(c.Entity, flagEntity)
}

// Span — флаги спанового вызова.
type Span struct {
	// Separators — исходный текст между соседними токенами спана, ровно
	// «токенов минус один» элемент. Без него каждый стык — один пробел.
	Separators []string
	// SentenceInitial — открывает ли спан предложение; влияет только на
	// однословный фолбэк.
	SentenceInitial bool
	// Protected возвращает собранную поверхность без единого поиска.
	Protected bool
}

func (s Span) flags() uint8 {
	return flagIf(s.SentenceInitial, flagSentenceInitial) | flagIf(s.Protected, flagProtected)
}

func flagIf(set bool, flag uint8) uint8 {
	if set {
		return flag
	}
	return 0
}

// Masks — необязательные маски пакетного вызова, каждая строго длины входа.
// Nil-маска — поведение по умолчанию: первый токен открывает предложение,
// защиты и entity-разметки нет.
type Masks struct {
	SentenceInitial []bool
	Protected       []bool
	Entities        []bool
}

// Probes — какие таблицы лексикону разрешено спрашивать; фиксируется при
// загрузке. Нулевая структура запрещает всё, по умолчанию — [DefaultProbes].
type Probes struct {
	UseEntities      bool
	UseEntitiesLower bool
	UseExact         bool
	UseLower         bool
	UseExactLower    bool
	UseExternal      bool
	UseExternalLower bool
	UseRules         bool
	Protected        bool
}

// DefaultProbes — профиль `udlex.load` без аргументов: корпусные таблицы,
// внешний словарь и суффиксные правила; таблица имён — только по флагу вызова.
func DefaultProbes() Probes {
	return Probes{
		UseExact:         true,
		UseLower:         true,
		UseExactLower:    true,
		UseExternal:      true,
		UseExternalLower: true,
		UseRules:         true,
	}
}

// Language — строка каталога бандла: что установлено и насколько оно хорошо.
// Accuracy читается только вместе с Baseline, разницу считает [Language.Gain].
type Language struct {
	ISO6393        string   `json:"iso639_3"`        // код ISO 639-3, по которому язык грузят
	Name           string   `json:"name"`            // человекочитаемое имя языка
	Lexicon        string   `json:"lexicon"`         // словарь за ним, обычно трибанк UD
	Variant        string   `json:"variant"`         // full, lookup, rules, corpus, identity
	TrainingTokens uint64   `json:"training_tokens"` // сколько размеченных токенов ушло в обучение
	Entries        uint64   `json:"entries"`         // сколько строк «форма → лемма» в таблицах
	Accuracy       *float64 `json:"accuracy"`        // точное совпадение на отложенной выборке
	Baseline       *float64 `json:"baseline"`        // сколько даёт копирование поверхности
	IsUseful       *bool    `json:"is_useful"`       // измерение показало выигрыш над копированием
	// Оценка на выборке, которая выбрала вариант (dev или train-cv): у
	// отгружаемых словарей test-измерения нет.
	SelectedOn        *string  `json:"selected_on"`
	SelectionAccuracy *float64 `json:"selection_accuracy"`
	SelectionBaseline *float64 `json:"selection_baseline"`
}

// Gain — сколько точности словарь добавляет к копированию поверхности.
// Второе значение — false, если хотя бы одна из сторон не измерялась.
func (l Language) Gain() (float64, bool) {
	if l.Accuracy == nil || l.Baseline == nil {
		return 0, false
	}
	return *l.Accuracy - *l.Baseline, true
}

// Meta — паспорт открытого словаря: те же поля, что свойства Lexicon в Python.
type Meta struct {
	Name           string   `json:"name"`        // идентификатор лексикона, обычно трибанк UD
	Language       string   `json:"language"`    // человекочитаемое имя языка
	ISO6393        string   `json:"iso639_3"`    // код языка по ISO 639-3
	Source         string   `json:"source"`      // из чего лексикон выучен
	Variant        string   `json:"variant"`     // какие части лексикона везёт бандл
	SelectedOn     *string  `json:"selected_on"` // какие данные выбрали вариант: dev или train-cv
	TrainingTokens uint64   `json:"training_tokens"`
	Entries        uint64   `json:"entries"`
	Accuracy       *float64 `json:"accuracy"`
	Baseline       *float64 `json:"baseline"`
	IsUseful       *bool    `json:"is_useful"`
	// С непустым user-слоем три поля выше nil: качество базы не выдаётся за
	// качество изменённых ответов. Base* хранят факты базы.
	BaseAccuracy      *float64 `json:"base_accuracy"`
	BaseBaseline      *float64 `json:"base_baseline"`
	BaseIsUseful      *bool    `json:"base_is_useful"`
	HasUserDictionary bool     `json:"has_user_dictionary"`
	UserEntries       uint64   `json:"user_entries"`
	UserGeneration    *string  `json:"user_generation"`
	Generation        *string  `json:"generation"`     // неизменяемое поколение, на которое смотрит current
	FormatVersion     uint32   `json:"format_version"` // версия раскладки на диске
	Normalization     string   `json:"normalization"`  // нормальная форма Unicode ключей и строк
	SpanCase          *string  `json:"span_case"`      // конвенция регистра спанов из поколения
	SpanMultiword     *string  `json:"span_multiword"` // конвенция многословных спанов из поколения
	// Действующие конвенции: отличаются от поколения только после
	// [WithSpanCase] или [WithSpanMultiword].
	EffectiveSpanCase      string `json:"effective_span_case"`
	EffectiveSpanMultiword string `json:"effective_span_multiword"`
	// Строчный ретрай таблицы имён в спановом пути: открыла ли его политика
	// поколения и действует ли он сейчас (явная опция побеждает поколение).
	EntityLower            bool `json:"entity_lower"`
	EffectiveEntitiesLower bool `json:"effective_entities_lower"`
}

// Code — что именно отказало. Коды ядра приходят из C ABI; CodeClosed и
// CodeABI добавляет сама Go-обвязка.
type Code int32

// Коды отказов.
const (
	CodeArg      Code = -1 // аргумент нарушает контракт
	CodeUTF8     Code = -2 // вход не UTF-8 или граница режет символ
	CodeOpen     Code = -3 // словарь не открылся
	CodeSurface  Code = -4 // поверхность спана пуста или с краевым пробелом
	CodeEntities Code = -5 // таблица именованных сущностей повреждена
	CodePanic    Code = -6 // паника внутри ядра, перехваченная на границе
	CodeClosed   Code = -7 // вызов к закрытому словарю; оборачивает [ErrClosed]
	CodeABI      Code = -8 // ядро вернуло неизвестное значение ABI
)

func (c Code) String() string {
	switch c {
	case CodeArg:
		return "аргумент"
	case CodeUTF8:
		return "не UTF-8"
	case CodeOpen:
		return "словарь не открылся"
	case CodeSurface:
		return "поверхность спана"
	case CodeEntities:
		return "таблица сущностей"
	case CodePanic:
		return "паника ядра"
	case CodeClosed:
		return "словарь закрыт"
	case CodeABI:
		return "нарушение ABI"
	default:
		return "код " + strconv.Itoa(int(c))
	}
}

// Error — отказ одного вызова. Все ошибки пакета имеют этот тип:
//
//	var failure *udlex.Error
//	if errors.As(err, &failure) && failure.Code == udlex.CodeSurface { … }
//
// Причина с собственным именем ещё и обёрнута: errors.Is(err, udlex.ErrClosed).
type Error struct {
	Op      string // какой вызов отказал
	Code    Code   // что именно отказало
	Message string // текст от ядра, уже с путями и причиной
	Err     error  // причина с собственным именем, если она есть
}

func (e *Error) Error() string {
	switch {
	case e.Message != "":
		return fmt.Sprintf("udlex: %s: %s", e.Op, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("udlex: %s: %s", e.Op, e.Err)
	default:
		return fmt.Sprintf("udlex: %s: %s", e.Op, e.Code)
	}
}

// Unwrap отдаёт именованную причину для errors.Is.
func (e *Error) Unwrap() error { return e.Err }
