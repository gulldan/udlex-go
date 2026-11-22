package udlex_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gulldan/udlex-go"
)

const fixtureBundle = "testdata/smoke-bundle"

func bundle(tb testing.TB) string {
	tb.Helper()
	path := os.Getenv(udlex.BundleVariable)
	if path == "" {
		return fixtureBundle
	}
	if _, err := os.Stat(path); err != nil {
		tb.Fatal(err)
	}
	return path
}

const lemmaKot = "кот"

var (
	loadedMutex sync.Mutex
	loaded      = map[string]*udlex.Lexicon{}
)

// lexicon открывает словарь один раз на процесс: открытие проверяет таблицы, а
// лексикон неизменяем.
func lexicon(tb testing.TB, language string) *udlex.Lexicon {
	tb.Helper()
	path := bundle(tb)
	loadedMutex.Lock()
	defer loadedMutex.Unlock()
	if lex, ok := loaded[language]; ok {
		return lex
	}
	lex, err := udlex.Load(path, language)
	if err != nil {
		if path == fixtureBundle && (language == "eng" || language == "rus") {
			tb.Fatalf("synthetic dictionary %s: %v", language, err)
		}
		tb.Skipf("словарь %s не открылся: %v", language, err)
	}
	loaded[language] = lex
	return lex
}

// loadRus открывает отдельный rus-словарь с опциями и закрывает его в конце теста.
func loadRus(t *testing.T, options ...udlex.Option) *udlex.Lexicon {
	t.Helper()
	lex, err := udlex.Load(bundle(t), "rus", options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lex.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return lex
}

func lemmaOf(tb testing.TB, lex *udlex.Lexicon, token string, call ...udlex.Call) string {
	tb.Helper()
	got, err := lex.Lemma(token, call...)
	if err != nil {
		tb.Fatalf("Lemma(%q): %v", token, err)
	}
	return got
}

func infoOf(tb testing.TB, lex *udlex.Lexicon, token string, call ...udlex.Call) udlex.Info {
	tb.Helper()
	got, err := lex.LemmaWithInfo(token, call...)
	if err != nil {
		tb.Fatalf("LemmaWithInfo(%q): %v", token, err)
	}
	return got
}

func lemmasOf(tb testing.TB, lex *udlex.Lexicon, tokens []string, masks ...udlex.Masks) []string {
	tb.Helper()
	got, err := lex.Lemmatize(tokens, masks...)
	if err != nil {
		tb.Fatalf("Lemmatize(%q): %v", tokens, err)
	}
	return got
}

func knowsOf(tb testing.TB, lex *udlex.Lexicon, token string) bool {
	tb.Helper()
	got, err := lex.Knows(token)
	if err != nil {
		tb.Fatalf("Knows(%q): %v", token, err)
	}
	return got
}

func assertCode(tb testing.TB, err error, code udlex.Code, what string) {
	tb.Helper()
	var failure *udlex.Error
	switch {
	case err == nil:
		tb.Fatalf("%s: ошибки нет, а должна быть", what)
	case !errors.As(err, &failure):
		tb.Fatalf("%s: %v — ожидался *udlex.Error", what, err)
	case failure.Code != code:
		tb.Fatalf("%s: код %s, ожидался %s (%v)", what, failure.Code, code, err)
	}
}

func TestVersionAndFormat(t *testing.T) {
	manifest, err := os.ReadFile("native/Cargo.toml")
	if err != nil {
		t.Fatal(err)
	}
	constants := udlex.Constants()
	if constants.Version == "" || constants.Version != udlex.Version() {
		t.Fatalf("версия в константах %q, в пакете %q", constants.Version, udlex.Version())
	}
	if !strings.Contains(string(manifest), "\nversion = "+strconv.Quote(constants.Version)+"\n") {
		t.Fatalf("версия ядра %q не совпадает с версией native/Cargo.toml", constants.Version)
	}
	if constants.FormatVersion == 0 || constants.MinFormatVersion == 0 || constants.FormatRange == "" ||
		constants.Normalization == "" || constants.EntityEncoding == "" || constants.PhraseCodec == "" ||
		len(constants.Sources) == 0 {
		t.Fatalf("константы формата не заполнены: %+v", constants)
	}
	if !udlex.FormatSupported(constants.FormatVersion) {
		t.Fatalf("сборка не читает формат %d, который сама же пишет", constants.FormatVersion)
	}
	if udlex.FormatSupported(constants.MinFormatVersion - 1) {
		t.Fatal("сборка объявила читаемым формат ниже своей нижней границы")
	}
	if remedy, err := udlex.FormatRemedy(constants.FormatVersion + 100); err != nil || remedy == "" {
		t.Fatalf("FormatRemedy: %q, %v", remedy, err)
	}
}

func TestCodeNames(t *testing.T) {
	for _, code := range []udlex.Code{
		udlex.CodeArg, udlex.CodeUTF8, udlex.CodeOpen, udlex.CodeSurface,
		udlex.CodeEntities, udlex.CodePanic, udlex.CodeClosed, udlex.CodeABI,
	} {
		if strings.HasPrefix(code.String(), "код ") {
			t.Errorf("код %d печатается как %q", code, code)
		}
	}
}

// Примеры README обязаны работать буквально.
func TestOneWord(t *testing.T) {
	cases := []struct{ language, token, want string }{
		{"rus", "котами", lemmaKot},
		{"ukr", "науковці", "науковець"},
		{"pol", "najlepszego", "dobry"},
		{"ces", "výsledky", "výsledek"},
		{"deu", "Häusern", "Haus"},
		{"eng", "mice", "mouse"},
		{"fra", "chercheurs", "chercheur"},
		{"spa", "investigadores", "investigador"},
		{"ita", "ricercatori", "ricercatore"},
		{"fin", "kirjoissamme", "kirja"},
		{"hun", "könyvekben", "könyv"},
		{"tur", "kitaplarımızda", "kitap"},
		{"kaz", "балаларға", "бала"},
		{"ell", "βιβλίων", "βιβλίο"},
		{"heb", "ספרים", "ספר"},
		{"hin", "किताबों", "किताब"},
		{"kat", "წიგნები", "წიგნი"},
		{"hye", "գիրքը", "գիրք"},
		{"jpn", "読ん", "読む"}, // токен уже от сегментатора вызывающего
	}
	for _, test := range cases {
		t.Run(test.language, func(t *testing.T) {
			if got := lemmaOf(t, lexicon(t, test.language), test.token); got != test.want {
				t.Fatalf("Lemma(%q) = %q, ожидалось %q", test.token, got, test.want)
			}
		})
	}
}

func TestSentence(t *testing.T) {
	cases := []struct {
		language     string
		tokens, want []string
	}{
		{"rus", []string{"Мама", "мыла", "раму"}, []string{"мама", "мыть", "рама"}},
		{
			"ukr",
			[]string{"Науковці", "опублікували", "результати", "нових", "досліджень"},
			[]string{"науковець", "опублікувати", "результат", "новий", "дослідження"},
		},
	}
	for _, test := range cases {
		t.Run(test.language, func(t *testing.T) {
			if got := lemmasOf(t, lexicon(t, test.language), test.tokens); !slices.Equal(got, test.want) {
				t.Fatalf("Lemmatize(%q) = %q, ожидалось %q", test.tokens, got, test.want)
			}
		})
	}
}

// Длина выхода — контракт: разметка вызывающего опирается на индексы. 32 «детей»
// (10 байт → «ребенок», 14) исчерпывают запас буфера и проходят через
// UDLEX_MORE_SPACE.
func TestLemmatizeKeepsLengthAndGrowsBuffer(t *testing.T) {
	lex := lexicon(t, "rus")
	for _, size := range []int{0, 1, 2, 7, 64, 513} {
		if got := lemmasOf(t, lex, slices.Repeat([]string{"котами"}, size)); len(got) != size {
			t.Fatalf("на входе %d токенов, на выходе %d", size, len(got))
		}
	}
	want := lemmaOf(t, lex, "детей")
	if len(want) <= len("детей") {
		t.Skipf("лемма %q не длиннее формы: рост буфера не проверить", want)
	}
	for index, lemma := range lemmasOf(t, lex, slices.Repeat([]string{"детей"}, 32))[1:] {
		if lemma != want {
			t.Fatalf("токен %d: %q, ожидалось %q", index+1, lemma, want)
		}
	}
}

// Пакетный и потокенный маршруты пересекают границу по-разному, а отвечать
// обязаны одинаково — и леммой, и провенансом.
func TestBatchAgreesWithSingle(t *testing.T) {
	lex := lexicon(t, "rus")
	tokens := strings.Fields(
		"Сервис деплоит микросервисы в кубернетесе и логирует метрики через " +
			"опентелеметрию а разработчики читают дашборды и мама мыла раму " +
			"котами детей человеку Пушкиным Нижнего Новгорода",
	)
	batch := lemmasOf(t, lex, tokens)
	infos, err := lex.LemmatizeWithInfo(tokens)
	if err != nil {
		t.Fatal(err)
	}
	for index, token := range tokens {
		want := infoOf(t, lex, token, udlex.Call{SentenceInitial: index == 0})
		if batch[index] != want.Lemma || infos[index] != want {
			t.Fatalf("токен %q: пакет дал %q / %+v, потокенно %+v", token, batch[index], infos[index], want)
		}
	}
}

func TestInfo(t *testing.T) {
	lex := lexicon(t, "rus")
	cases := map[string]udlex.Info{
		"кот":      {Lemma: "кот", Source: udlex.SourceExact},
		"Пушкиным": {Lemma: "Пушкин", Source: udlex.SourceExact},
		"дашборды": {Lemma: "дашборды", Source: udlex.SourceIdentity},
	}
	if bundle(t) != fixtureBundle {
		cases["логирует"] = udlex.Info{Lemma: "логировать", Source: udlex.SourceRule}
	}
	for token, want := range cases {
		if got := infoOf(t, lex, token); got != want {
			t.Errorf("LemmaWithInfo(%q) = %+v, ожидалось %+v", token, got, want)
		}
	}
	if got := infoOf(t, lex, "котами", udlex.Call{Protected: true}); got !=
		(udlex.Info{Lemma: "котами", Source: udlex.SourceProtected}) {
		t.Errorf("защищённый ответ: %+v", got)
	}
	if !knowsOf(t, lex, "котами") || knowsOf(t, lex, "дашборды") {
		t.Error("Knows: котами должен быть известен, дашборды — нет")
	}
}

func TestSourceContract(t *testing.T) {
	cases := []struct {
		name              string
		lemmatized, known bool
	}{
		{"entity", true, true},
		{"entity_lower", true, true},
		{"exact", true, true},
		{"lower", true, true},
		{"exact_lower", true, true},
		{"external", true, true},
		{"external_lower", true, true},
		{"rule", true, false},
		{"identity", false, false},
		{"protected", false, false},
		{"composed", true, false},
		{"phrase", true, false},
		{"user", true, true},
		{"user_lower", true, true},
		{"context", true, true},
	}
	if len(cases) != int(udlex.SourceContext)+1 {
		t.Fatalf("таблица покрывает %d кодов из %d", len(cases), udlex.SourceContext+1)
	}
	for code, test := range cases {
		source := udlex.Source(code)
		if !source.IsValid() || source.String() != test.name ||
			source.IsLemmatized() != test.lemmatized || source.IsKnown() != test.known {
			t.Errorf("код %d: %s valid=%v lemmatized=%v known=%v, ожидалось %+v",
				code, source, source.IsValid(), source.IsLemmatized(), source.IsKnown(), test)
		}
	}
	unknown := udlex.Source(255)
	if unknown.IsValid() || unknown.String() != "source(255)" || unknown.IsKnown() || unknown.IsLemmatized() {
		t.Fatalf("неизвестный код должен быть консервативным: %s", unknown)
	}
}

func userBundleFixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLayeredUserDictionaryContract(t *testing.T) {
	userBundle := userBundleFixture(t, "user-bundle")
	lex := loadRus(t, udlex.WithUserDictionary(userBundle))

	identity := infoOf(t, lex, "котами")
	if identity != (udlex.Info{Lemma: "котами", Source: udlex.SourceUser}) {
		t.Fatalf("identity override = %+v", identity)
	}
	if !identity.Source.IsKnown() || !knowsOf(t, lex, "котами") {
		t.Fatal("identity override обязан быть известным словарным ответом")
	}
	if lower := infoOf(t, lex, "ДАШБОРДЫ"); lower != (udlex.Info{Lemma: "ручнойдашборд", Source: udlex.SourceUserLower}) {
		t.Fatalf("lower override = %+v", lower)
	}
	if protected := infoOf(t, lex, "котами", udlex.Call{Protected: true}); protected.Source != udlex.SourceProtected {
		t.Fatalf("protected затронул user layer: %+v", protected)
	}
	if entity := infoOf(t, lex, "котами", udlex.Call{Entity: true}); entity.Source == udlex.SourceUser ||
		entity.Source == udlex.SourceUserLower {
		t.Fatalf("entity-only увидел user layer: %+v", entity)
	}

	infos, err := lex.LemmatizeWithInfo([]string{"котами", "ДАШБОРДЫ"})
	if err != nil {
		t.Fatal(err)
	}
	want := []udlex.Info{
		{Lemma: "котами", Source: udlex.SourceUser},
		{Lemma: "ручнойдашборд", Source: udlex.SourceUserLower},
	}
	if !slices.Equal(infos, want) {
		t.Fatalf("batch provenance = %+v, ожидалось %+v", infos, want)
	}

	meta, baseMeta := lex.Meta(), lexicon(t, "rus").Meta()
	if !meta.HasUserDictionary || meta.UserEntries != 2 || meta.UserGeneration == nil {
		t.Fatalf("layered metadata неполна: %+v", meta)
	}
	if meta.Accuracy != nil || meta.Baseline != nil || meta.IsUseful != nil {
		t.Fatalf("composite quality выдала base score: %+v", meta)
	}
	if !reflect.DeepEqual(meta.BaseAccuracy, baseMeta.Accuracy) ||
		!reflect.DeepEqual(meta.BaseBaseline, baseMeta.Baseline) ||
		!reflect.DeepEqual(meta.BaseIsUseful, baseMeta.IsUseful) {
		t.Fatalf("base quality изменилась под layer: layered=%+v base=%+v", meta, baseMeta)
	}

	disabled := loadRus(t, udlex.WithUserDictionary(userBundle), udlex.WithoutUserDictionary())
	if got := infoOf(t, disabled, "котами"); got.Source == udlex.SourceUser || got.Source == udlex.SourceUserLower {
		t.Fatalf("WithoutUserDictionary не отключил lookup: %+v", got)
	}
	if !disabled.Meta().HasUserDictionary {
		t.Fatal("диагностический opt-out скрыл аутентифицированный layer")
	}
}

func TestLayeredOpenAndFailClosedRoots(t *testing.T) {
	lex, err := udlex.Open(
		filepath.Join(bundle(t), lexicon(t, "rus").Meta().Name),
		udlex.WithUserDictionary(userBundleFixture(t, "user-bundle")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := infoOf(t, lex, "котами"); got.Source != udlex.SourceUser {
		t.Fatalf("Open layered дал %+v", got)
	}
	if err = lex.Close(); err != nil {
		t.Fatal(err)
	}

	write := func(path, content string) {
		t.Helper()
		if failure := errors.Join(os.MkdirAll(filepath.Dir(path), 0o700), os.WriteFile(path, []byte(content), 0o600)); failure != nil {
			t.Fatal(failure)
		}
	}
	const header = "{\"schema\":\"udlex-user-dictionary/v1\"}\n"
	corrupt, empty := t.TempDir(), t.TempDir()
	write(filepath.Join(corrupt, "user-bundle.json"), header)
	write(filepath.Join(corrupt, "rus", "current"), "u00000000000000000000000000000000\n")
	write(filepath.Join(empty, "user-bundle.json"), header)

	_, err = udlex.Load(bundle(t), "rus", udlex.WithUserDictionary(filepath.Join(t.TempDir(), "missing")))
	assertCode(t, err, udlex.CodeOpen, "отсутствующий user root")
	_, err = udlex.Load(bundle(t), "rus", udlex.WithUserDictionary(corrupt), udlex.WithoutUserDictionary())
	assertCode(t, err, udlex.CodeOpen, "повреждённый layer за use_user=false")

	if loadRus(t, udlex.WithUserDictionary(empty)).Meta().HasUserDictionary {
		t.Fatal("валидный root без rus объявлен активным layer")
	}
	emptyMeta := loadRus(t, udlex.WithUserDictionary(userBundleFixture(t, "empty-user-bundle"))).Meta()
	if emptyMeta.HasUserDictionary || emptyMeta.UserEntries != 0 || emptyMeta.UserGeneration == nil {
		t.Fatalf("пустое поколение не стало наблюдаемым base-only: %+v", emptyMeta)
	}
	if !reflect.DeepEqual(emptyMeta.Accuracy, emptyMeta.BaseAccuracy) ||
		!reflect.DeepEqual(emptyMeta.Baseline, emptyMeta.BaseBaseline) ||
		!reflect.DeepEqual(emptyMeta.IsUseful, emptyMeta.BaseIsUseful) {
		t.Fatalf("пустое поколение скрыло base quality: %+v", emptyMeta)
	}
}

func TestMasks(t *testing.T) {
	lex := lexicon(t, "rus")
	got := lemmasOf(t, lex, []string{"в", "Нью-Йорке", "ACME"}, udlex.Masks{
		Entities:  []bool{false, true, false},
		Protected: []bool{false, false, true},
	})
	if want := []string{"в", "Нью-Йорк", "ACME"}; !slices.Equal(got, want) {
		t.Fatalf("с масками получилось %q, ожидалось %q", got, want)
	}
	// Маска не длины входа — отказ, а не усечение, как ValueError в Python.
	for _, mask := range []udlex.Masks{
		{Protected: []bool{true}},
		{Entities: []bool{true, false, true}},
		{SentenceInitial: []bool{}},
	} {
		_, err := lex.Lemmatize([]string{"а", "б"}, mask)
		assertCode(t, err, udlex.CodeArg, "Lemmatize с маской не той длины")
		_, err = lex.LemmatizeWithInfo([]string{"а", "б"}, mask)
		assertCode(t, err, udlex.CodeArg, "LemmatizeWithInfo с маской не той длины")
	}
}

func TestSentenceMaskDoesNotOpenEntities(t *testing.T) {
	lex := loadRus(t)
	tokens := []string{"Мама", "мыла", "раму", "Кот", "спал"}
	got := lemmasOf(t, lex, tokens, udlex.Masks{SentenceInitial: []bool{true, false, false, true, false}})
	if want := []string{"мама", "мыть", "рама", "кот", "спать"}; !slices.Equal(got, want) {
		t.Fatalf("с маской предложений %q, ожидалось %q", got, want)
	}
	if lex.EntitiesReady() {
		t.Fatal("маска SentenceInitial открыла таблицу сущностей")
	}
	lemmasOf(t, lex, tokens, udlex.Masks{Entities: []bool{false, false, false, true, false}})
	if !lex.EntitiesReady() {
		t.Fatal("entity-строка пакета не открыла таблицу")
	}
	if err := lex.EnsureEntities(); err != nil {
		t.Fatal(err)
	}
}

// Контекст меняет только точный ответ, у которого словарь выучил ключ; где
// ключа нет, ответ тот же, что у Lemmatize.
func TestLemmatizeContext(t *testing.T) {
	lex := lexicon(t, "rus")
	tokens := []string{"Мама", "мыла", "раму", "в", "Нью-Йорке"}
	masks := udlex.Masks{Entities: []bool{false, false, false, false, true}}
	plain, err := lex.LemmatizeWithInfo(tokens, masks)
	if err != nil {
		t.Fatal(err)
	}
	upos := []string{"NOUN", "VERB", "NOUN", "ADP", "PROPN"}
	got, err := lex.LemmatizeContextWithInfo(tokens, upos, true, masks)
	if err != nil {
		t.Fatal(err)
	}
	for index, info := range got {
		if info != plain[index] && info.Source != udlex.SourceContext {
			t.Fatalf("%q: %+v, без контекста %+v", tokens[index], info, plain[index])
		}
	}
	if got, err = lex.LemmatizeContextWithInfo(tokens, nil, false, masks); err != nil || !slices.Equal(got, plain) {
		t.Fatalf("без подсказок %+v (%v), ожидалось %+v", got, err, plain)
	}
	lemmas, err := lex.LemmatizeContext(tokens, nil, false)
	if want := lemmasOf(t, lex, tokens); err != nil || !slices.Equal(lemmas, want) {
		t.Fatalf("без контекста %q (%v), ожидалось %q", lemmas, err, want)
	}
	for _, tags := range [][]string{upos[:2], {}} {
		_, err = lex.LemmatizeContext(tokens, tags, false)
		assertCode(t, err, udlex.CodeArg, "LemmatizeContext")
		_, err = lex.LemmatizeContextWithInfo(tokens, tags, false)
		assertCode(t, err, udlex.CodeArg, "LemmatizeContextWithInfo")
	}
}

func TestSecondOptionStructIsError(t *testing.T) {
	lex := lexicon(t, "rus")
	two := []udlex.Call{{SentenceInitial: true}, {Protected: true}}
	twoSpans := []udlex.Span{{}, {Protected: true}}

	_, err := lex.Lemma("котами", two...)
	assertCode(t, err, udlex.CodeArg, "Lemma")
	_, err = lex.LemmaWithInfo("котами", two...)
	assertCode(t, err, udlex.CodeArg, "LemmaWithInfo")
	_, err = lex.Knows("котами", two...)
	assertCode(t, err, udlex.CodeArg, "Knows")
	_, err = lex.LemmaSpanText("Нижнего Новгорода", twoSpans...)
	assertCode(t, err, udlex.CodeArg, "LemmaSpanText")
	_, err = lex.LemmaSpan([]string{"Нижнего", "Новгорода"}, 0, 2, twoSpans...)
	assertCode(t, err, udlex.CodeArg, "LemmaSpan")
	_, err = lex.Lemmatize([]string{"котами"}, udlex.Masks{}, udlex.Masks{})
	assertCode(t, err, udlex.CodeArg, "Lemmatize")
}

func TestSpanText(t *testing.T) {
	lex := lexicon(t, "rus")
	for text, want := range map[string]string{
		"Ленинградской области":                     "Ленинградская область",
		"Нижнего Новгорода":                         "Нижний Новгород",
		"Московского государственного университета": "Московский государственный университет",
		// Непокрытое возвращается байт в байт, с двойным, табуляцией и NBSP.
		"Зюзюкин  Кабанчиковский":     "Зюзюкин  Кабанчиковский",
		"Зюзюкин\tКабанчиковский":     "Зюзюкин\tКабанчиковский",
		"Зюзюкин\u00a0Кабанчиковский": "Зюзюкин\u00a0Кабанчиковский",
	} {
		if got, err := lex.LemmaSpanText(text); err != nil || got != want {
			t.Errorf("LemmaSpanText(%q) = %q (%v), ожидалось %q", text, got, err, want)
		}
	}
	for _, text := range []string{"", " ", "\t", " Москва", "Москва "} {
		_, err := lex.LemmaSpanText(text)
		assertCode(t, err, udlex.CodeSurface, strconv.Quote(text))
	}
	_, err := lex.LemmaSpanText("Нижнего Новгорода", udlex.Span{Separators: []string{" "}})
	assertCode(t, err, udlex.CodeArg, "Separators у текстового спана")
}

// Оба ответа совпадают с поверхностью, но один — запись газеттира, второй —
// пословная сборка: строкой Source не заменить. Случай опубликован в README.
func TestDocumentedUnchangedSpanProvenance(t *testing.T) {
	lex := lexicon(t, "rus")
	for _, want := range []udlex.Info{
		{Lemma: "Северного потока", Source: udlex.SourceEntity},
		{Lemma: "щзхъ йцукен", Source: udlex.SourceComposed},
	} {
		if got, err := lex.LemmaSpanTextWithInfo(want.Lemma); err != nil || got != want {
			t.Errorf("LemmaSpanTextWithInfo(%q) = %+v (%v), ожидалось %+v", want.Lemma, got, err, want)
		}
	}
}

func TestSpanTokens(t *testing.T) {
	rus := lexicon(t, "rus")
	cases := []struct {
		lex        *udlex.Lexicon
		tokens     []string
		start, end int
		span       []udlex.Span
		want       string
	}{
		{rus, []string{"поехал", "в", "Нижний", "Новгород"}, 2, 4, nil, "Нижний Новгород"},
		{rus, []string{"Нью", "Йорка"}, 0, 2, []udlex.Span{{Separators: []string{"-"}}}, "Нью-Йорк"},
	}
	for _, test := range cases {
		if got, err := test.lex.LemmaSpan(test.tokens, test.start, test.end, test.span...); err != nil || got != test.want {
			t.Errorf("LemmaSpan(%q) = %q (%v), ожидалось %q", test.tokens, got, err, test.want)
		}
	}
	if bundle(t) != fixtureBundle {
		t.Run("pol", func(t *testing.T) {
			lex := lexicon(t, "pol")
			got, err := lex.LemmaSpan([]string{"studiował", "na", "Uniwersytecie", "Warszawskim"}, 2, 4)
			if err != nil || got != "Uniwersytet Warszawski" {
				t.Errorf("LemmaSpan = %q (%v)", got, err)
			}
		})
	}
	info, err := rus.LemmaSpanWithInfo([]string{"Нижнего", "Новгорода"}, 0, 2)
	if err != nil || info != (udlex.Info{Lemma: "Нижний Новгород", Source: udlex.SourceEntity}) {
		t.Fatalf("провенанс спана: %+v (%v)", info, err)
	}

	for _, bad := range []struct {
		start, end int
		span       udlex.Span
	}{
		{1, 1, udlex.Span{}},  // пустой
		{2, 1, udlex.Span{}},  // перевёрнутый
		{0, 5, udlex.Span{}},  // за краем
		{-1, 2, udlex.Span{}}, // отрицательный
		{0, 2, udlex.Span{Separators: []string{" ", " "}}},
	} {
		_, err := rus.LemmaSpan([]string{"а", "б"}, bad.start, bad.end, bad.span)
		assertCode(t, err, udlex.CodeArg, "негодный спан")
	}
}

// Строка Go может не быть UTF-8; выдать её за лемму значило бы соврать про
// источник ответа.
func TestInvalidUTF8IsError(t *testing.T) {
	lex := lexicon(t, "rus")
	broken := string([]byte{0xff, 0xfe})
	for name, call := range map[string]func() error{
		"Lemma":             func() error { _, err := lex.Lemma(broken); return err },
		"Lemmatize":         func() error { _, err := lex.Lemmatize([]string{"Мама", broken, "раму"}); return err },
		"LemmatizeWithInfo": func() error { _, err := lex.LemmatizeWithInfo([]string{broken}); return err },
		"Knows":             func() error { _, err := lex.Knows(broken); return err },
		"LemmaSpanText":     func() error { _, err := lex.LemmaSpanText(broken); return err },
		"LemmaSpan":         func() error { _, err := lex.LemmaSpan([]string{broken}, 0, 1); return err },
	} {
		assertCode(t, call(), udlex.CodeUTF8, name)
	}
}

func TestLoadAndOpen(t *testing.T) {
	_, err := udlex.Load(bundle(t), "zzz")
	assertCode(t, err, udlex.CodeOpen, "Load(zzz)")

	lex := lexicon(t, "rus")
	direct, err := udlex.Open(filepath.Join(bundle(t), lex.Meta().Name))
	if err != nil {
		t.Fatalf("прямое открытие каталога: %v", err)
	}
	defer direct.Close()
	if lemmaOf(t, direct, "котами") != lemmaOf(t, lex, "котами") {
		t.Fatal("Open и Load открыли разное")
	}
}

func TestLoadInstalled(t *testing.T) {
	path := bundle(t)
	t.Setenv(udlex.BundleVariable, "")
	if got := udlex.BundlePath(); got != udlex.DefaultBundle {
		t.Fatalf("без переменной окружения BundlePath() = %q", got)
	}

	t.Setenv(udlex.BundleVariable, path)
	if got := udlex.BundlePath(); got != path {
		t.Fatalf("BundlePath() = %q", got)
	}
	lex, err := udlex.LoadInstalled("rus")
	if err != nil {
		t.Fatal(err)
	}
	defer lex.Close()
	if got := lemmaOf(t, lex, "котами"); got != lemmaKot {
		t.Fatalf("установленный словарь ответил %q", got)
	}
	if languages, listErr := udlex.SupportedLanguages(); listErr != nil || len(languages) == 0 {
		t.Fatalf("SupportedLanguages(): %d языков, %v", len(languages), listErr)
	}

	_, err = udlex.LoadInstalled("ru")
	if err == nil || !strings.Contains(err.Error(), "did you mean rus?") ||
		!strings.Contains(err.Error(), udlex.BundleVariable+"=") {
		t.Fatalf("LoadInstalled(ru): %v", err)
	}
	t.Setenv(udlex.BundleVariable, t.TempDir())
	if _, err := udlex.LoadInstalled("rus"); err == nil || !strings.Contains(err.Error(), "manifest.json") {
		t.Fatalf("LoadInstalled в пустом каталоге: %v", err)
	}
}

func TestMeta(t *testing.T) {
	meta := lexicon(t, "rus").Meta()
	if meta.ISO6393 != "rus" || meta.Name == "" || meta.Language == "" ||
		meta.Entries == 0 || meta.TrainingTokens == 0 ||
		meta.FormatVersion == 0 || meta.Normalization == "" ||
		meta.EffectiveSpanCase == "" || meta.EffectiveSpanMultiword == "" {
		t.Fatalf("паспорт словаря неполон: %+v", meta)
	}
}

// Переопределение конвенций есть в сигнатуре Python — значит, и здесь, и
// доезжает до ядра.
func TestSpanConventionOverride(t *testing.T) {
	meta := loadRus(t,
		udlex.WithSpanCase(udlex.SpanCaseVerbatim),
		udlex.WithSpanMultiword(udlex.SpanMultiwordCompose)).Meta()
	if meta.EffectiveSpanCase != udlex.SpanCaseVerbatim || meta.EffectiveSpanMultiword != udlex.SpanMultiwordCompose {
		t.Fatalf("конвенции не применились: %q %q", meta.EffectiveSpanCase, meta.EffectiveSpanMultiword)
	}
	_, err := udlex.Load(bundle(t), "rus", udlex.WithSpanCase("наискосок"))
	assertCode(t, err, udlex.CodeArg, "неизвестная конвенция")
}

func TestLanguagesCatalog(t *testing.T) {
	languages, err := udlex.Languages(bundle(t))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSortedFunc(languages, func(a, b udlex.Language) int { return strings.Compare(a.ISO6393, b.ISO6393) }) {
		t.Fatal("каталог не отсортирован")
	}
	index := slices.IndexFunc(languages, func(language udlex.Language) bool { return language.ISO6393 == "rus" })
	if index < 0 || languages[index].Lexicon == "" || languages[index].Name == "" {
		t.Fatalf("строки rus в каталоге нет или она неполна: %d", index)
	}
	if _, err := udlex.Languages(filepath.Join(bundle(t), "не-бандл")); err == nil {
		t.Fatal("каталог несуществующего пути прочитался")
	}
}

func TestGainNeedsBothSides(t *testing.T) {
	accuracy := 0.9
	if _, measured := (udlex.Language{Accuracy: &accuracy}).Gain(); measured {
		t.Fatal("Gain() без базовой линии обязан отвечать «не измерено»")
	}
}

func TestProbeOptions(t *testing.T) {
	// «логирует» знает только суффиксное правило.
	withoutRules := loadRus(t, udlex.WithoutRules())
	if got := lemmaOf(t, withoutRules, "логирует"); got != "логирует" {
		t.Fatalf("без правил получилось %q", got)
	}
	if probes := withoutRules.Probes(); !probes.UseExact || probes.UseRules {
		t.Fatalf("профиль не тот: %+v", probes)
	}
	if got := lemmaOf(t, loadRus(t, udlex.AlwaysProtected()), "котами"); got != "котами" {
		t.Fatalf("защищённый лексикон вернул %q", got)
	}

	byDefault := loadRus(t)
	if meta := byDefault.Meta(); meta.EffectiveEntitiesLower != meta.EntityLower || byDefault.Probes().UseEntitiesLower {
		t.Fatalf("спановый ретрай не взят из поколения или протёк в профиль токена: %+v", meta)
	}
	if !loadRus(t, udlex.WithEntitiesLower()).Meta().EffectiveEntitiesLower {
		t.Fatal("явный WithEntitiesLower не включил ретрай")
	}
}

func TestClosedLexiconReturnsErrClosed(t *testing.T) {
	lex, err := udlex.Load(bundle(t), "rus")
	if err != nil {
		t.Fatal(err)
	}
	if err := lex.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lex.Close(); err != nil {
		t.Fatalf("повторный Close: %v", err)
	}

	span := []string{"Нижнего", "Новгорода"}
	calls := map[string]func() error{
		"Lemma":                 func() error { _, err := lex.Lemma("котами"); return err },
		"LemmaWithInfo":         func() error { _, err := lex.LemmaWithInfo("котами"); return err },
		"Knows":                 func() error { _, err := lex.Knows("котами"); return err },
		"Lemmatize":             func() error { _, err := lex.Lemmatize([]string{"котами"}); return err },
		"LemmatizeWithInfo":     func() error { _, err := lex.LemmatizeWithInfo([]string{"котами"}); return err },
		"LemmaSpanText":         func() error { _, err := lex.LemmaSpanText("Нижнего Новгорода"); return err },
		"LemmaSpanTextWithInfo": func() error { _, err := lex.LemmaSpanTextWithInfo("Нижнего Новгорода"); return err },
		"LemmaSpan":             func() error { _, err := lex.LemmaSpan(span, 0, 2); return err },
		"LemmaSpanWithInfo":     func() error { _, err := lex.LemmaSpanWithInfo(span, 0, 2); return err },
		"EnsureEntities":        lex.EnsureEntities,
	}
	for name, call := range calls {
		err := call()
		if !errors.Is(err, udlex.ErrClosed) {
			t.Errorf("%s после Close: errors.Is(err, ErrClosed) = false, err = %v", name, err)
		}
		assertCode(t, err, udlex.CodeClosed, name)
	}
	if lex.EntitiesReady() {
		t.Error("EntitiesReady() у закрытого словаря вернул true")
	}
}

// Лексикон обещан для любого числа горутин, разные языки ничего не делят. Смысл
// тест имеет под -race: ловит гонку и в пуле буферов.
func TestConcurrentUse(t *testing.T) {
	probes := []struct{ language, token, want string }{
		{"rus", "котами", lemmaKot},
		{"eng", "mice", "mouse"},
		{"ukr", "науковці", "науковець"},
		{"pol", "najlepszego", "dobry"},
		{"fin", "kirjoissamme", "kirja"},
		{"deu", "Häusern", "Haus"},
	}
	if bundle(t) == fixtureBundle {
		probes = probes[:2]
	}
	lexicons := make([]*udlex.Lexicon, len(probes))
	for index, probe := range probes {
		lexicons[index] = lexicon(t, probe.language)
	}
	rus := lexicons[0]
	tokens := strings.Fields("Мама мыла раму котами Пушкиным логирует дашборды детей")
	want := lemmasOf(t, rus, tokens)

	var group sync.WaitGroup
	for worker := range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := range 40 {
				index := (worker + round) % len(probes)
				probe, lex := probes[index], lexicons[index]
				if got, err := lex.Lemma(probe.token); err != nil || got != probe.want {
					t.Errorf("горутина %d, %s: %q (%v), ожидалось %q", worker, probe.language, got, err, probe.want)
					return
				}
				if got, err := rus.Lemmatize(tokens); err != nil || !slices.Equal(got, want) {
					t.Errorf("горутина %d: %q (%v), ожидалось %q", worker, got, err, want)
					return
				}
				if _, err := rus.LemmaSpanText("Нижнего Новгорода"); err != nil {
					t.Errorf("горутина %d: %v", worker, err)
					return
				}
			}
		}()
	}
	group.Wait()
}

func TestLemmatizeInto(t *testing.T) {
	lex := lexicon(t, "rus")
	dst := make([]string, 0, 8)
	for _, tokens := range [][]string{
		{"Мама", "мыла", "раму"},
		{"котами"},
		{},
		{"Мама", "мыла", "раму", "котами", "детей"},
	} {
		var err error
		if dst, err = lex.LemmatizeInto(dst, tokens); err != nil {
			t.Fatal(err)
		}
		if len(dst) != len(tokens) {
			t.Fatalf("на %d токенов вернулось %d лемм", len(tokens), len(dst))
		}
		for index, token := range tokens {
			if want := lemmaOf(t, lex, token, udlex.Call{SentenceInitial: index == 0}); dst[index] != want {
				t.Fatalf("токен %q: %q вместо %q", token, dst[index], want)
			}
		}
	}
	// При ошибке dst не меняется — ни содержимое, ни массив под ним.
	for _, capacity := range []int{1, 8} {
		dst := make([]string, 1, capacity)
		dst[0] = "unchanged"
		got, err := lex.LemmatizeInto(dst, []string{"котами", string([]byte{0xff})})
		assertCode(t, err, udlex.CodeUTF8, "LemmatizeInto")
		if len(got) != 1 || got[0] != "unchanged" || cap(got) != cap(dst) || &got[0] != &dst[0] {
			t.Fatalf("capacity %d: dst изменился после ошибки: %q", capacity, got)
		}
	}
}

// Пустая строка — законный токен: буфер и смещения вырождаются, ответ — пустая строка.
func TestEmptyTokens(t *testing.T) {
	lex := lexicon(t, "rus")
	if got := lemmaOf(t, lex, ""); got != "" {
		t.Fatalf("пустой токен стал %q", got)
	}
	if got := lemmasOf(t, lex, []string{"", "мыла", ""}); !slices.Equal(got, []string{"", "мыть", ""}) {
		t.Fatalf("пакет с пустыми токенами = %q", got)
	}
	if got := lemmasOf(t, lex, []string{"", "", ""}); !slices.Equal(got, []string{"", "", ""}) {
		t.Fatalf("пакет из одних пустых токенов = %q", got)
	}
}

// Освобождение забытого лексикона из уборки не должно задевать общее состояние.
func TestForgottenCloseIsCollected(t *testing.T) {
	func() {
		lex, err := udlex.Load(bundle(t), "rus")
		if err != nil {
			t.Fatal(err)
		}
		if lemmaOf(t, lex, "котами") != lemmaKot {
			t.Fatal("словарь открылся, но отвечает не то")
		}
	}()
	for range 3 {
		runtime.GC()
	}
	runtime.Gosched()
	if lemmaOf(t, lexicon(t, "rus"), "котами") != lemmaKot {
		t.Fatal("после уборки чужого лексикона свой отвечает не то")
	}
}

// Собранный обратно текст отличается от исходного ровно леммами: gaps едут байт
// в байт.
func TestAppendLemmatized(t *testing.T) {
	lex := lexicon(t, "rus")
	tokens := []string{"Мама", "мыла", "раму"}
	cases := []struct {
		dst  string
		gaps []string
		want string
	}{
		{"", []string{"  ", " ", ", а потом\t", ".\n"}, "  мама мыть, а потом\tрама.\n"},
		{"", nil, "мама мыть рама"},
		{"вот: ", nil, "вот: мама мыть рама"},
	}
	for _, test := range cases {
		if got, err := lex.AppendLemmatized([]byte(test.dst), tokens, test.gaps); err != nil || string(got) != test.want {
			t.Errorf("AppendLemmatized(%q, %q) = %q (%v), ожидалось %q", test.dst, test.gaps, got, err, test.want)
		}
	}

	long := strings.Fields("Сервис деплоит микросервисы в кубернетесе и логирует метрики " +
		"через опентелеметрию а разработчики читают дашборды детей")
	got, err := lex.AppendLemmatized(nil, long, nil)
	if want := strings.Join(lemmasOf(t, lex, long), " "); err != nil || string(got) != want {
		t.Fatalf("текстовый путь дал %q (%v), потокенный — %q", got, err, want)
	}

	_, err = lex.AppendLemmatized(nil, []string{"а", "б"}, []string{" "})
	assertCode(t, err, udlex.CodeArg, "AppendLemmatized с gaps не той длины")
}

// Обещания про аллокации проверены счётчиком. Под -race он меряет детектор.
func TestAllocations(t *testing.T) {
	if raceDetector {
		t.Skip("детектор гонок аллоцирует сам")
	}
	lex := lexicon(t, "rus")
	allocs := func(call func() error) float64 {
		return testing.AllocsPerRun(200, func() {
			if err := call(); err != nil {
				t.Fatal(err)
			}
		})
	}

	text := strings.Fields(strings.Repeat("Вчера вечером научные сотрудники института опубликовали "+
		"результаты новых исследований о климате северных областей ", 4))
	out := make([]byte, 0, 4096)
	if got := allocs(func() (err error) { out, err = lex.AppendLemmatized(out[:0], text, nil); return err }); got != 0 {
		t.Errorf("AppendLemmatized: %.1f аллокаций, ожидалось 0", got)
	}

	// Lemma аллоцирует только изменившуюся лемму; совпавшая — входная строка.
	for token, want := range map[string]float64{"дашборды": 0, lemmaKot: 0, "котами": 1} {
		if got := allocs(func() error { _, err := lex.Lemma(token); return err }); got != want {
			t.Errorf("Lemma(%q): %.1f аллокаций, ожидалось %.0f", token, got, want)
		}
	}
	if got := allocs(func() error { _, err := lex.Knows("котами"); return err }); got != 0 {
		t.Errorf("Knows: %.1f аллокаций, ожидалось 0", got)
	}

	tokens := strings.Fields("Мама мыла раму котами и дашборды")
	changed := 0
	for index, lemma := range lemmasOf(t, lex, tokens) {
		if lemma != tokens[index] {
			changed++
		}
	}
	dst := make([]string, len(tokens))
	if got := allocs(func() (err error) { dst, err = lex.LemmatizeInto(dst, tokens); return err }); int(got) != changed {
		t.Errorf("LemmatizeInto: %.1f аллокаций при %d изменившихся леммах", got, changed)
	}
}
