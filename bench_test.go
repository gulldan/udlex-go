package udlex_test

import (
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gulldan/udlex-go"
)

// Связный текст: частотные короткие слова, которые лежат в кэше.
const runningText = "Вчера вечером научные сотрудники института опубликовали результаты " +
	"новых исследований и рассказали журналистам о том как менялся климат " +
	"в северных областях страны за последние тридцать лет "

func benchTokens() []string {
	return strings.Fields(strings.Repeat("Мама мыла раму Кот спал котами детей неизвестное ", 40))
}

func report(b *testing.B, tokens int) {
	b.Helper()
	b.ReportMetric(float64(b.N*tokens)/b.Elapsed().Seconds(), "токенов/с")
}

// Потокенный вызов: здесь видна цена перехода границы, а не поиска.
func BenchmarkLemma(b *testing.B) {
	lex := lexicon(b, "rus")
	tokens := benchTokens()
	b.ReportAllocs()
	index := 0
	for b.Loop() {
		if _, err := lex.Lemma(tokens[index%len(tokens)]); err != nil {
			b.Fatal(err)
		}
		index++
	}
	report(b, 1)
}

func BenchmarkKnows(b *testing.B) {
	lex := lexicon(b, "rus")
	tokens := benchTokens()
	b.ReportAllocs()
	index := 0
	for b.Loop() {
		if _, err := lex.Knows(tokens[index%len(tokens)]); err != nil {
			b.Fatal(err)
		}
		index++
	}
	report(b, 1)
}

// Пакет: один переход границы на предложение.
func BenchmarkLemmatize(b *testing.B) {
	lex := lexicon(b, "rus")
	all := benchTokens()
	for _, size := range []int{1, 8, 32, 300} {
		tokens := all[:min(size, len(all))]
		b.Run(strconv.Itoa(len(tokens))+"_токенов", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := lex.Lemmatize(tokens); err != nil {
					b.Fatal(err)
				}
			}
			report(b, len(tokens))
		})
	}
}

// С переиспользованием среза аллоцируются только изменившиеся леммы.
func BenchmarkLemmatizeInto(b *testing.B) {
	lex := lexicon(b, "rus")
	for name, tokens := range map[string][]string{
		"словарь": benchTokens(),
		"текст":   strings.Fields(strings.Repeat(runningText, 8)),
	} {
		b.Run(name, func(b *testing.B) {
			dst := make([]string, len(tokens))
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if dst, err = lex.LemmatizeInto(dst, tokens); err != nil {
					b.Fatal(err)
				}
			}
			report(b, len(tokens))
		})
	}
}

func BenchmarkLemmatizeWithInfo(b *testing.B) {
	lex := lexicon(b, "rus")
	tokens := benchTokens()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lex.LemmatizeWithInfo(tokens); err != nil {
			b.Fatal(err)
		}
	}
	report(b, len(tokens))
}

// Ноль аллокаций здесь проверяет TestAllocations.
func BenchmarkAppendLemmatized(b *testing.B) {
	lex := lexicon(b, "rus")
	tokens := strings.Fields(strings.Repeat(runningText, 8))
	out := make([]byte, 0, 8192)
	b.ReportAllocs()
	for b.Loop() {
		var err error
		if out, err = lex.AppendLemmatized(out[:0], tokens, nil); err != nil {
			b.Fatal(err)
		}
	}
	report(b, len(tokens))
}

func BenchmarkLemmaSpanText(b *testing.B) {
	lex := lexicon(b, "rus")
	if err := lex.EnsureEntities(); err != nil {
		b.Skipf("таблица сущностей не открылась: %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lex.LemmaSpanText("Нижнего Новгорода"); err != nil {
			b.Fatal(err)
		}
	}
}

// Горутины в одном словаре: общий на горячем пути только пул буферов.
func BenchmarkLemmatizeParallel(b *testing.B) {
	lex := lexicon(b, "rus")
	tokens := benchTokens()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		dst := make([]string, len(tokens))
		for pb.Next() {
			var err error
			if dst, err = lex.LemmatizeInto(dst, tokens); err != nil {
				b.Fatal(err)
			}
		}
	})
	report(b, len(tokens))
}

func BenchmarkLanguagesParallel(b *testing.B) {
	lexicons := []*udlex.Lexicon{lexicon(b, "rus"), lexicon(b, "eng")}
	corpora := [][]string{
		benchTokens(),
		strings.Fields(strings.Repeat("cats mice unknown dogs cats mice birds unknown ", 40)),
	}
	var next atomic.Int64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		index := int(next.Add(1)-1) % len(lexicons)
		lex, tokens := lexicons[index], corpora[index]
		dst := make([]string, len(tokens))
		for pb.Next() {
			var err error
			if dst, err = lex.LemmatizeInto(dst, tokens); err != nil {
				b.Fatal(err)
			}
		}
	})
	report(b, len(corpora[0]))
}
