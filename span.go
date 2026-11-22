package udlex

/*
#include "udlex.h"
*/
import "C"

import (
	"runtime"
	"strconv"
)

const (
	opLemmaSpan     = "lemma_span"
	opLemmaSpanText = "lemma_span_text"
)

// LemmaSpanText возвращает каноническую форму упоминания NER, пришедшего одной
// строкой.
//
//	lex.LemmaSpanText("Ленинградской области")  // Ленинградская область
//
// Пробельные пробеги поверхности сохраняются байт в байт: если ни одна таблица
// не ответила, вернётся ровно ваша строка. Пустая поверхность, из одних
// пробелов или с краевым пробелом — отказ [CodeSurface], ничего не подрезается.
// [Span].Separators здесь не задают: зазоры берутся из самого текста.
func (l *Lexicon) LemmaSpanText(text string, span ...Span) (string, error) {
	info, err := l.LemmaSpanTextWithInfo(text, span...)
	return info.Lemma, err
}

// LemmaSpanTextWithInfo — то же с провенансом ответа.
func (l *Lexicon) LemmaSpanTextWithInfo(text string, span ...Span) (Info, error) {
	options, err := one(opLemmaSpanText, "Span", span)
	if err != nil {
		return Info{}, err
	}
	if options.Separators != nil {
		return Info{}, &Error{
			Op:      opLemmaSpanText,
			Code:    CodeArg,
			Message: "Separators здесь не задают: зазоры берутся из самого текста",
		}
	}
	ptr, err := l.pointer(opLemmaSpanText)
	if err != nil {
		return Info{}, err
	}
	buf := l.grab()
	defer l.pool.Put(buf)
	buf.out = grow(buf.out, len(text)+outputSlack)
	for range attempts {
		status := C.udlex_lemma_span_text(ptr, stringPtr(text), C.size_t(len(text)),
			C.uint8_t(options.flags()), bytePtr(buf.out), C.size_t(len(buf.out)),
			&buf.length, &buf.source, &buf.cerr)
		runtime.KeepAlive(l)
		if status != statusMoreSpace {
			return answer(opLemmaSpanText, text, status, buf)
		}
		buf.out = grow(buf.out, int(buf.length))
	}
	return Info{}, errShortBuffer(opLemmaSpanText)
}

// LemmaSpan возвращает каноническую форму упоминания, заданного полуинтервалом
// токенов [start, end).
//
//	lex.LemmaSpan([]string{"поехал", "в", "Нижний", "Новгород"}, 2, 4)
//	// Нижний Новгород
//
// Исходный текст между токенами спана задаётся в [Span].Separators — ровно
// end-start-1 элемент; без него каждый стык становится одним пробелом.
// Пустой или вышедший за границы интервал и неверная длина Separators —
// ошибка [CodeArg].
func (l *Lexicon) LemmaSpan(tokens []string, start, end int, span ...Span) (string, error) {
	info, err := l.LemmaSpanWithInfo(tokens, start, end, span...)
	return info.Lemma, err
}

// LemmaSpanWithInfo — то же с провенансом ответа.
func (l *Lexicon) LemmaSpanWithInfo(tokens []string, start, end int, span ...Span) (Info, error) {
	options, err := one(opLemmaSpan, "Span", span)
	if err != nil {
		return Info{}, err
	}
	parts, err := spanParts(tokens, start, end, options)
	if err != nil {
		return Info{}, err
	}
	ptr, err := l.pointer(opLemmaSpan)
	if err != nil {
		return Info{}, err
	}
	buf := l.grab()
	defer l.pool.Put(buf)

	var ok bool
	if buf.in, buf.offsets, ok = pack(buf.in, buf.offsets, parts); !ok {
		return Info{}, &Error{Op: opLemmaSpan, Code: CodeArg, Message: "спан длиннее 4 ГиБ"}
	}
	hasSeparators := C.uint8_t(0)
	if options.Separators != nil {
		hasSeparators = 1
		if buf.sepIn, buf.sepOff, ok = pack(buf.sepIn, buf.sepOff, options.Separators); !ok {
			return Info{}, &Error{Op: opLemmaSpan, Code: CodeArg, Message: "зазоры длиннее 4 ГиБ"}
		}
	}
	buf.out = grow(buf.out, len(buf.in)+len(buf.sepIn)+outputSlack)
	for range attempts {
		status := C.udlex_lemma_span(ptr,
			bytePtr(buf.in), C.size_t(len(buf.in)),
			offsetPtr(buf.offsets), C.size_t(len(parts)),
			bytePtr(buf.sepIn), C.size_t(len(buf.sepIn)),
			offsetPtr(buf.sepOff), C.size_t(len(options.Separators)),
			hasSeparators, C.uint8_t(options.flags()),
			bytePtr(buf.out), C.size_t(len(buf.out)),
			&buf.length, &buf.source, &buf.cerr)
		runtime.KeepAlive(l)
		if status == statusSame {
			// Поверхность склеивает ядро: SAME здесь — нарушение протокола.
			return Info{}, fail(opLemmaSpan, status, buf.cerr)
		}
		if status != statusMoreSpace {
			return answer(opLemmaSpan, "", status, buf)
		}
		buf.out = grow(buf.out, int(buf.length))
	}
	return Info{}, errShortBuffer(opLemmaSpan)
}

// spanParts проверяет интервал и список зазоров и отдаёт токены спана.
func spanParts(tokens []string, start, end int, span Span) ([]string, error) {
	if start < 0 || end > len(tokens) || start >= end {
		return nil, &Error{
			Op:   opLemmaSpan,
			Code: CodeArg,
			Message: "спан [" + strconv.Itoa(start) + ", " + strconv.Itoa(end) +
				") пуст или вне границ " + strconv.Itoa(len(tokens)) + " токенов",
		}
	}
	parts := tokens[start:end]
	if span.Separators != nil && len(span.Separators) != len(parts)-1 {
		return nil, &Error{
			Op:   opLemmaSpan,
			Code: CodeArg,
			Message: "Separators: " + strconv.Itoa(len(span.Separators)) +
				" элементов, ожидалось " + strconv.Itoa(len(parts)-1),
		}
	}
	return parts, nil
}
