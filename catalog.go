package udlex

/*
#include "udlex.h"
*/
import "C"

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// BundleVariable — переменная окружения, которой задают каталог со словарями.
const BundleVariable = "UDLEX_BUNDLE"

// DefaultBundle — куда смотреть, когда её нет.
const DefaultBundle = "bundle"

// BundlePath возвращает каталог, в котором лежат установленные словари.
// Использует [BundleVariable], если она задана, иначе — [DefaultBundle].
func BundlePath() string {
	return cmp.Or(os.Getenv(BundleVariable), DefaultBundle)
}

// LoadInstalled открывает словарь из [BundlePath] — то же, что
// `udlex.load("rus")` в Python. Если словаря нет, ошибка называет каталог,
// состояние UDLEX_BUNDLE, что установлено и какой язык, вероятно, имелся в виду.
func LoadInstalled(language string, options ...Option) (*Lexicon, error) {
	bundle := BundlePath()
	lexicon, err := Load(bundle, language, options...)
	if err != nil {
		return nil, explainMissing(bundle, language, err)
	}
	return lexicon, nil
}

// explainMissing дописывает к отказу открытия то, что нужно, чтобы его исправить.
func explainMissing(bundle, language string, err error) error {
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != CodeOpen {
		return err
	}
	where, _ := filepath.Abs(bundle)
	origin := BundleVariable + " не задана"
	if configured := os.Getenv(BundleVariable); configured != "" {
		origin = BundleVariable + "=" + configured
	}
	listed, listErr := Languages(bundle)
	if listErr != nil || len(listed) == 0 {
		failure.Message = "словарей нет в " + where + " (" + origin + "); скачайте архив " +
			"словаря из релиза и распакуйте его так, чтобы получился " + where +
			"/manifest.json, или укажите каталог в " + BundleVariable
		return failure
	}
	codes := make([]string, 0, len(listed))
	for _, entry := range listed {
		codes = append(codes, entry.ISO6393)
	}
	hint := ""
	if meant := suggest(language, listed); meant != "" && meant != language {
		hint = " (did you mean " + meant + "?)"
	}
	failure.Message = "в " + where + " (" + origin + ") нет словаря для " + language + hint +
		"; установлены: " + strings.Join(codes, ", ")
	return failure
}

// suggest — тот единственный язык каталога, который мог иметься в виду: код,
// имя лексикона, английское название или его ISO 639-1-префикс (ru из ru_taiga).
func suggest(name string, listed []Language) string {
	key := strings.ToLower(name)
	found := ""
	for _, entry := range listed {
		prefix, _, _ := strings.Cut(entry.Lexicon, "_")
		if key != entry.ISO6393 && key != entry.Lexicon && key != strings.ToLower(entry.Name) && key != prefix {
			continue
		}
		if found != "" && found != entry.ISO6393 {
			return ""
		}
		found = entry.ISO6393
	}
	return found
}

// Languages перечисляет языки бандла. Читает только метаданные, таблиц поиска
// не открывает.
func Languages(bundle string) ([]Language, error) {
	var out, cerr C.udlex_bytes
	status := C.udlex_languages_json(stringPtr(bundle), C.size_t(len(bundle)), &out, &cerr)
	runtime.KeepAlive(bundle)
	if status != statusOK {
		return nil, fail("languages", status, cerr)
	}
	var languages []Language
	if err := json.Unmarshal([]byte(take(out)), &languages); err != nil {
		return nil, &Error{Op: "languages", Message: err.Error()}
	}
	return languages, nil
}

// SupportedLanguages — [Languages] для [BundlePath].
func SupportedLanguages() ([]Language, error) { return Languages(BundlePath()) }

// Format — константы раскладки на диске. Ядро пишет одну версию формата и
// читает диапазон; что делать при отказе, говорит [FormatRemedy].
type Format struct {
	Version          string   `json:"version"`            // версия ядра, из которого собрана библиотека
	FormatVersion    uint32   `json:"format_version"`     // раскладка, которую эта сборка пишет
	MinFormatVersion uint32   `json:"min_format_version"` // самая старая, которую ещё читает
	FormatRange      string   `json:"format_range"`       // диапазон одной записью: «4» или «4-6»
	MinCoreRelease   string   `json:"min_core_release"`   // нижняя граница пина словаря на ядро
	EntityEncoding   string   `json:"entity_encoding"`    // контракт ключей таблицы имён собственных
	Normalization    string   `json:"normalization"`      // нормальная форма Unicode ключей и строк
	PhraseCodec      string   `json:"phrase_codec"`       // кодировка таблицы фразовых шаблонов
	PhraseSeparator  string   `json:"phrase_separator"`   // разделитель полей фразового ключа
	PhraseTail       int      `json:"phrase_tail"`        // сколько конечных символов токена в ключе
	Sources          []string `json:"sources"`            // исходные файлы формата
}

//nolint:gochecknoglobals // константы ядра десериализуются один раз по требованию
var constants = sync.OnceValue(readConstants)

func readConstants() Format {
	var out, cerr C.udlex_bytes
	if status := C.udlex_constants_json(&out, &cerr); status != statusOK {
		C.udlex_bytes_free(cerr)
		return Format{}
	}
	var format Format
	if err := json.Unmarshal([]byte(take(out)), &format); err != nil {
		return Format{}
	}
	return format
}

// Constants возвращает константы раскладки на диске.
func Constants() Format { return constants() }

// Version — версия ядра, из которого собрана слинкованная библиотека.
func Version() string { return constants().Version }

// FormatSupported отвечает, читает ли эта сборка бандл такой версии раскладки.
func FormatSupported(formatVersion uint32) bool {
	return C.udlex_format_supported(C.uint32_t(formatVersion)) != 0
}

// FormatRemedy говорит, что делать с форматом, который эта сборка не читает:
// обновить ядро или пересобрать словарь.
func FormatRemedy(formatVersion uint32) (string, error) {
	var out, cerr C.udlex_bytes
	if status := C.udlex_format_remedy(C.uint32_t(formatVersion), &out, &cerr); status != statusOK {
		return "", fail("format_remedy", status, cerr)
	}
	return take(out), nil
}
