package udlex

/*
#include "udlex.h"
*/
import "C"

import (
	"math"
	"strconv"
	"unsafe"
)

// Биты флагов вызова — из заголовка, чтобы две стороны не разошлись.
const (
	flagSentenceInitial = uint8(C.UDLEX_SENTENCE_INITIAL)
	flagProtected       = uint8(C.UDLEX_PROTECTED)
	flagEntity          = uint8(C.UDLEX_ENTITY)
)

// Коды возврата, которые не ошибка.
const (
	statusOK        = C.int32_t(C.UDLEX_OK)
	statusSame      = C.int32_t(C.UDLEX_SAME)
	statusMoreSpace = C.int32_t(C.UDLEX_MORE_SPACE)
)

// bytePtr отдаёт указатель на начало среза или nil для пустого. Go-память без
// Go-указателей можно отдавать в C, пока C её не сохраняет — это контракт
// заголовка.
func bytePtr(buffer []byte) *C.uint8_t {
	if len(buffer) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&buffer[0]))
}

func offsetPtr(buffer []uint32) *C.uint32_t {
	if len(buffer) == 0 {
		return nil
	}
	return (*C.uint32_t)(unsafe.Pointer(&buffer[0]))
}

// stringPtr отдаёт байты строки без копии. Пустая строка даёт NULL — для ядра
// это «аргумент не задан».
func stringPtr(text string) *C.uint8_t {
	if len(text) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(unsafe.StringData(text)))
}

// take копирует владеемый ядром буфер в строку и освобождает его.
func take(owned C.udlex_bytes) string {
	if owned.ptr == nil || owned.len == 0 {
		return ""
	}
	text := C.GoStringN((*C.char)(unsafe.Pointer(owned.ptr)), C.int(owned.len))
	C.udlex_bytes_free(owned)
	return text
}

func fail(op string, code C.int32_t, owned C.udlex_bytes) error {
	return &Error{Op: op, Code: Code(code), Message: take(owned)}
}

// cOptions переводит профиль в структуру C. Строчный ретрай, который никто не
// задал явно, уходит как UDLEX_PROBE_DEFAULT: решает политика поколения.
func cOptions(probes Probes, lowerChosen bool) C.udlex_options {
	lower := C.uint8_t(C.UDLEX_PROBE_DEFAULT)
	if lowerChosen {
		lower = cFlag(probes.UseEntitiesLower)
	}
	return C.udlex_options{
		use_entities:       cFlag(probes.UseEntities),
		use_entities_lower: lower,
		use_exact:          cFlag(probes.UseExact),
		use_lower:          cFlag(probes.UseLower),
		use_exact_lower:    cFlag(probes.UseExactLower),
		use_external:       cFlag(probes.UseExternal),
		use_external_lower: cFlag(probes.UseExternalLower),
		use_rules:          cFlag(probes.UseRules),
		protected_:         cFlag(probes.Protected),
	}
}

func cFlag(value bool) C.uint8_t {
	if value {
		return 1
	}
	return 0
}

const (
	// Запас выходного буфера сверх длины входа: лемма обычно короче формы.
	outputSlack = 16
	// Вторая попытка идёт с точным размером, который назвало ядро.
	attempts = 2
)

// buffers — переиспользуемая память одного вызова.
type buffers struct {
	in      []byte   // токены подряд
	offsets []uint32 // границы токенов, len+1
	sepIn   []byte   // зазоры спана подряд
	sepOff  []uint32 // границы зазоров
	uposIn  []byte   // теги UPOS подряд
	uposOff []uint32 // границы тегов
	out     []byte   // место под ответы
	outOff  []uint32 // границы ответов
	same    []byte   // ответ равен входу: отдать входную строку без копии
	sources []byte   // финальная ступень по токену

	// Выходные параметры C живут в пуле, чтобы cgo не уводил локальные в кучу.
	length C.size_t
	single C.uint8_t
	source C.udlex_source
	cerr   C.udlex_bytes
}

func (l *Lexicon) grab() *buffers {
	if buf, ok := l.pool.Get().(*buffers); ok {
		return buf
	}
	return &buffers{}
}

// answer разбирает итог одиночного вызова ядра, кроме UDLEX_MORE_SPACE:
// UDLEX_SAME значит, что ответ — сам surface.
func answer(op, surface string, status C.int32_t, buf *buffers) (Info, error) {
	switch status {
	case statusSame:
	case statusOK:
		surface = string(buf.out[:buf.length])
	default:
		return Info{}, fail(op, status, buf.cerr)
	}
	source, err := decodeSource(op, uint8(buf.source))
	if err != nil {
		return Info{}, err
	}
	return Info{Lemma: surface, Source: source}, nil
}

// grow подгоняет длину среза под нужную, переиспользуя ёмкость.
func grow[T any](buffer []T, need int) []T {
	if cap(buffer) >= need {
		return buffer[:need]
	}
	return make([]T, need)
}

// pack раскладывает строки в непрерывный буфер и массив границ.
func pack(data []byte, offsets []uint32, values []string) ([]byte, []uint32, bool) {
	data = data[:0]
	offsets = append(offsets[:0], 0)
	for _, value := range values {
		data = append(data, value...)
		if len(data) > math.MaxUint32 {
			return data, offsets, false
		}
		offsets = append(offsets, uint32(len(data)))
	}
	return data, offsets, true
}

func decodeSource(op string, code uint8) (Source, error) {
	source := Source(code)
	if source.IsValid() {
		return source, nil
	}
	return 0, &Error{
		Op:      op,
		Code:    CodeABI,
		Message: "ядро вернуло неизвестный код Source " + strconv.Itoa(int(code)),
	}
}

func errShortBuffer(op string) error {
	return &Error{Op: op, Code: CodeArg, Message: "выходной буфер не сошёлся с двух попыток"}
}
