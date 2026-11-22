package udlex

/*
#include "udlex.h"
*/
import "C"

import (
	"runtime"
	"strconv"
)

// Lemmatize лемматизирует уже разбитое на токены предложение.
//
//	lex.Lemmatize([]string{"Мама", "мыла", "раму"})  // [мама мыть рама]
//
// На выходе ровно столько же строк, сколько на входе. Первый токен считается
// началом предложения; изменить это и разметить защищённые токены и сущности
// можно через [Masks].
func (l *Lexicon) Lemmatize(tokens []string, masks ...Masks) ([]string, error) {
	return l.LemmatizeInto(nil, tokens, masks...)
}

// LemmatizeInto — та же операция с переиспользованием dst. Без масок и с
// достаточной ёмкостью dst аллоцирует только изменившиеся леммы. При ошибке dst
// не меняется.
func (l *Lexicon) LemmatizeInto(dst []string, tokens []string, masks ...Masks) ([]string, error) {
	return l.lemmatizeInto(dst, tokens, masks, nil)
}

// LemmatizeWithInfo лемматизирует пакет и возвращает провенанс каждого ответа
// за один переход границы.
func (l *Lexicon) LemmatizeWithInfo(tokens []string, masks ...Masks) ([]Info, error) {
	return l.lemmatizeWithInfo(tokens, masks, nil)
}

// LemmatizeContext — [Lexicon.Lemmatize] с тем, что вызывающий знает о
// предложении: upos — тег UPOS на токен (nil — не спрашивать, пустой тег
// пропускается), context — спрашивать ключи соседних слов; граница предложения
// из [Masks] для них — край. Точный ответ уступает ключу контекста, выученному
// для неоднозначной формы.
//
//	lex.LemmatizeContext([]string{"Из", "стали"}, []string{"ADP", "NOUN"}, false)
func (l *Lexicon) LemmatizeContext(
	tokens, upos []string, context bool, masks ...Masks,
) ([]string, error) {
	if err := checkUPOS(tokens, upos); err != nil {
		return nil, err
	}
	lemmas, err := l.lemmatizeInto(make([]string, len(tokens)), tokens, masks, &sentence{upos, context})
	if err != nil {
		return nil, err
	}
	return lemmas, nil
}

// LemmatizeContextWithInfo — [Lexicon.LemmatizeContext] с провенансом: ответ
// ключа контекста приходит с [SourceContext].
func (l *Lexicon) LemmatizeContextWithInfo(
	tokens, upos []string, context bool, masks ...Masks,
) ([]Info, error) {
	if err := checkUPOS(tokens, upos); err != nil {
		return nil, err
	}
	return l.lemmatizeWithInfo(tokens, masks, &sentence{upos, context})
}

// sentence — контекст предложения для LemmatizeContext.
type sentence struct {
	upos       []string
	neighbours bool
}

func checkUPOS(tokens, upos []string) error {
	if upos != nil && len(upos) != len(tokens) {
		return &Error{
			Op:   "lemmatize_context",
			Code: CodeArg,
			Message: "upos не длины входа: " + strconv.Itoa(len(upos)) +
				" вместо " + strconv.Itoa(len(tokens)),
		}
	}
	return nil
}

func (l *Lexicon) lemmatizeInto(dst, tokens []string, masks []Masks, context *sentence) ([]string, error) {
	flags, err := packMasks(masks, len(tokens))
	if err != nil {
		return dst, err
	}
	result := grow(dst, len(tokens))
	if err := l.batch(result, nil, tokens, flags, context); err != nil {
		return dst, err
	}
	return result, nil
}

func (l *Lexicon) lemmatizeWithInfo(tokens []string, masks []Masks, context *sentence) ([]Info, error) {
	flags, err := packMasks(masks, len(tokens))
	if err != nil {
		return nil, err
	}
	infos := make([]Info, len(tokens))
	if err := l.batch(nil, infos, tokens, flags, context); err != nil {
		return nil, err
	}
	return infos, nil
}

// packMasks сводит маски в один байт флагов на токен. Маска не длины входа —
// ошибка, а не усечение: усечение молча сдвинуло бы разметку.
func packMasks(masks []Masks, count int) ([]byte, error) {
	if len(masks) == 0 {
		return nil, nil // горячий путь: проверка масок стоит ~20 нс на вызов
	}
	mask, err := one("lemmatize", "Masks", masks)
	if err != nil {
		return nil, err
	}
	if err := mask.validate(count); err != nil {
		return nil, err
	}
	if mask.SentenceInitial == nil && mask.Protected == nil && mask.Entities == nil {
		return nil, nil
	}
	flags := make([]byte, count)
	for index := range flags {
		flags[index] = mask.flagsAt(index)
	}
	return flags, nil
}

func (m Masks) validate(count int) error {
	named := [...]struct {
		name   string
		values []bool
	}{
		{"SentenceInitial", m.SentenceInitial},
		{"Protected", m.Protected},
		{"Entities", m.Entities},
	}
	for _, mask := range named {
		if mask.values != nil && len(mask.values) != count {
			return &Error{
				Op:   "lemmatize",
				Code: CodeArg,
				Message: "маска " + mask.name + " не длины входа: " +
					strconv.Itoa(len(mask.values)) + " вместо " + strconv.Itoa(count),
			}
		}
	}
	return nil
}

func (m Masks) flagsAt(index int) uint8 {
	var flags uint8
	if (m.SentenceInitial == nil && index == 0) || (m.SentenceInitial != nil && m.SentenceInitial[index]) {
		flags |= flagSentenceInitial
	}
	if m.Protected != nil && m.Protected[index] {
		flags |= flagProtected
	}
	if m.Entities != nil && m.Entities[index] {
		flags |= flagEntity
	}
	return flags
}

// batch раскладывает ответы пакета по dst и/или infos. Пустой пакет ядро не
// зовёт: на закрытом словаре он тоже успех.
func (l *Lexicon) batch(dst []string, infos []Info, tokens []string, flags []byte, context *sentence) error {
	if len(tokens) == 0 {
		return nil
	}
	buf := l.grab()
	defer l.pool.Put(buf)
	if err := l.probeBatch(buf, tokens, flags, "lemmatize", infos != nil, context); err != nil {
		return err
	}
	// Совпавшая с формой лемма не копируется: возвращается входная строка.
	start := uint32(0)
	for index, token := range tokens {
		end := buf.outOff[index+1]
		lemma := token
		if buf.same[index] == 0 {
			lemma = string(buf.out[start:end])
		}
		if dst != nil {
			dst[index] = lemma
		}
		if infos != nil {
			source, err := decodeSource("lemmatize", buf.sources[index])
			if err != nil {
				return err
			}
			infos[index] = Info{Lemma: lemma, Source: source}
		}
		start = end
	}
	return nil
}

// probeBatch прогоняет пакет через ядро и оставляет ответы в buf.
func (l *Lexicon) probeBatch(
	buf *buffers, tokens []string, flags []byte, op string, wantSources bool, context *sentence,
) error {
	ptr, err := l.pointer(op)
	if err != nil {
		return err
	}
	var ok bool
	if buf.in, buf.offsets, ok = pack(buf.in, buf.offsets, tokens); !ok {
		return &Error{Op: op, Code: CodeArg, Message: "пакет длиннее 4 ГиБ"}
	}
	buf.outOff = grow(buf.outOff, len(tokens)+1)
	buf.same = grow(buf.same, len(tokens))
	if wantSources {
		buf.sources = grow(buf.sources, len(tokens))
	} else {
		buf.sources = buf.sources[:0]
	}
	buf.out = grow(buf.out, len(buf.in)+outputSlack)
	var upos *C.uint32_t
	buf.uposIn = buf.uposIn[:0]
	if context != nil && context.upos != nil {
		if buf.uposIn, buf.uposOff, ok = pack(buf.uposIn, buf.uposOff, context.upos); !ok {
			return &Error{Op: op, Code: CodeArg, Message: "теги длиннее 4 ГиБ"}
		}
		upos = offsetPtr(buf.uposOff)
	}

	for range attempts {
		var status C.int32_t
		if context == nil || (context.upos == nil && !context.neighbours) {
			status = C.udlex_lemmatize(ptr,
				bytePtr(buf.in), C.size_t(len(buf.in)),
				offsetPtr(buf.offsets), C.size_t(len(tokens)),
				bytePtr(flags),
				bytePtr(buf.out), C.size_t(len(buf.out)),
				offsetPtr(buf.outOff), bytePtr(buf.same), bytePtr(buf.sources),
				&buf.length, &buf.cerr)
		} else {
			status = C.udlex_lemmatize_context(ptr,
				bytePtr(buf.in), C.size_t(len(buf.in)),
				offsetPtr(buf.offsets), C.size_t(len(tokens)),
				bytePtr(flags),
				bytePtr(buf.uposIn), C.size_t(len(buf.uposIn)), upos,
				cFlag(context.neighbours),
				bytePtr(buf.out), C.size_t(len(buf.out)),
				offsetPtr(buf.outOff), bytePtr(buf.same), bytePtr(buf.sources),
				&buf.length, &buf.cerr)
		}
		runtime.KeepAlive(l)
		switch status {
		case statusOK:
			return nil
		case statusMoreSpace:
			buf.out = grow(buf.out, int(buf.length))
		default:
			return fail(op, status, buf.cerr)
		}
	}
	return errShortBuffer(op)
}

// AppendLemmatized дописывает к dst лемматизированный текст и возвращает
// выросший срез, как append. Без масок и с достаточной ёмкостью dst Go-аллокаций
// нет.
//
//	out, err = lex.AppendLemmatized(out[:0], tokens, gaps)
//
// gaps — текст до первого токена, между соседними и после последнего, то есть
// на один элемент больше, чем токенов; собранный обратно текст отличается от
// исходного только леммами. nil — склеить одним пробелом, без краёв.
func (l *Lexicon) AppendLemmatized(
	dst []byte, tokens []string, gaps []string, masks ...Masks,
) ([]byte, error) {
	flags, err := packMasks(masks, len(tokens))
	if err != nil {
		return dst, err
	}
	if gaps != nil && len(gaps) != len(tokens)+1 {
		return dst, &Error{
			Op:   "append_lemmatized",
			Code: CodeArg,
			Message: "gaps: " + strconv.Itoa(len(gaps)) + " элементов, ожидалось " +
				strconv.Itoa(len(tokens)+1) + " — по одному до, между и после токенов",
		}
	}
	if len(tokens) == 0 {
		if len(gaps) == 1 {
			dst = append(dst, gaps[0]...)
		}
		return dst, nil
	}
	buf := l.grab()
	defer l.pool.Put(buf)
	if err := l.probeBatch(buf, tokens, flags, "append_lemmatized", false, nil); err != nil {
		return dst, err
	}

	start := uint32(0)
	for index, token := range tokens {
		switch {
		case gaps != nil:
			dst = append(dst, gaps[index]...)
		case index > 0:
			dst = append(dst, ' ')
		}
		end := buf.outOff[index+1]
		if buf.same[index] != 0 {
			dst = append(dst, token...)
		} else {
			dst = append(dst, buf.out[start:end]...)
		}
		start = end
	}
	if gaps != nil {
		dst = append(dst, gaps[len(tokens)]...)
	}
	return dst, nil
}
