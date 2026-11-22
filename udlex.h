/* Rust C ABI used by the Go bindings.
 *
 * The caller supplies output buffers. UDLEX_MORE_SPACE returns the required
 * capacity in out_len. Discard partial output and retry with that capacity.
 * Free library-owned udlex_bytes with udlex_bytes_free.
 * Callers must keep buffers disjoint and dictionary files immutable during use.
 * Wait for all readers before closing a lexicon.
 */

#ifndef UDLEX_H
#define UDLEX_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Коды возврата. */
#define UDLEX_OK 0
#define UDLEX_SAME 1       /* ответ равен входу; в выход ничего не записано */
#define UDLEX_MORE_SPACE 2 /* мал выходной буфер; нужный размер в out_len   */
#define UDLEX_ERR_ARG -1
#define UDLEX_ERR_UTF8 -2
#define UDLEX_ERR_OPEN -3
#define UDLEX_ERR_SURFACE -4
#define UDLEX_ERR_ENTITIES -5
#define UDLEX_ERR_PANIC -6

/* Биты флагов вызова. */
#define UDLEX_SENTENCE_INITIAL 1
#define UDLEX_PROTECTED 2
#define UDLEX_ENTITY 4

/* Один закрытый набор источников результата. uint8_t фиксирует размер ABI;
 * неизвестный код является нарушением контракта, а не новым fallback. */
typedef uint8_t udlex_source;
#define UDLEX_SOURCE_ENTITY ((udlex_source)0)
#define UDLEX_SOURCE_ENTITY_LOWER ((udlex_source)1)
#define UDLEX_SOURCE_EXACT ((udlex_source)2)
#define UDLEX_SOURCE_LOWER ((udlex_source)3)
#define UDLEX_SOURCE_EXACT_LOWER ((udlex_source)4)
#define UDLEX_SOURCE_EXTERNAL ((udlex_source)5)
#define UDLEX_SOURCE_EXTERNAL_LOWER ((udlex_source)6)
#define UDLEX_SOURCE_RULE ((udlex_source)7)
#define UDLEX_SOURCE_IDENTITY ((udlex_source)8)
#define UDLEX_SOURCE_PROTECTED ((udlex_source)9)
#define UDLEX_SOURCE_COMPOSED ((udlex_source)10)
#define UDLEX_SOURCE_PHRASE ((udlex_source)11)
#define UDLEX_SOURCE_USER ((udlex_source)12)
#define UDLEX_SOURCE_USER_LOWER ((udlex_source)13)
#define UDLEX_SOURCE_CONTEXT ((udlex_source)14)

typedef struct udlex_lexicon udlex_lexicon;

/* Буфер, которым владеет библиотека. */
typedef struct {
  uint8_t *ptr;
  size_t len;
} udlex_bytes;

/* Значение use_entities_lower: «как решила измеренная политика поколения». */
#define UDLEX_PROBE_DEFAULT 2

/* Какие таблицы разрешено спрашивать. NULL вместо структуры — профиль по
 * умолчанию: корпусные таблицы, внешний словарь, суффиксные правила и
 * строчный ретрай сущностей там, где его открыла измеренная политика. */
typedef struct {
  uint8_t use_entities;
  uint8_t use_entities_lower;
  uint8_t use_exact;
  uint8_t use_lower;
  uint8_t use_exact_lower;
  uint8_t use_external;
  uint8_t use_external_lower;
  uint8_t use_rules;
  uint8_t protected_;
} udlex_options;

const char *udlex_version(void);
void udlex_bytes_free(udlex_bytes bytes);

/* Константы формата одним JSON-объектом: версии раскладки, кодировки,
 * список исходных файлов. */
int32_t udlex_constants_json(udlex_bytes *out, udlex_bytes *err);
uint8_t udlex_format_supported(uint32_t format_version);
int32_t udlex_format_remedy(uint32_t format_version, udlex_bytes *out,
                            udlex_bytes *err);

/* span_case ("transfer"/"verbatim") и span_multiword ("identity"/"compose"/
 * "head") переопределяют измеренные конвенции спанов. NULL — оставить то, что
 * несёт поколение; это и есть продакшен. */
int32_t udlex_open(const uint8_t *path, size_t path_len,
                   const udlex_options *options, const uint8_t *span_case,
                   size_t span_case_len, const uint8_t *span_multiword,
                   size_t span_multiword_len, udlex_lexicon **out,
                   udlex_bytes *err);

int32_t udlex_load(const uint8_t *bundle, size_t bundle_len,
                   const uint8_t *language, size_t language_len,
                   const udlex_options *options, const uint8_t *span_case,
                   size_t span_case_len, const uint8_t *span_multiword,
                   size_t span_multiword_len, udlex_lexicon **out,
                   udlex_bytes *err);

/* Явные layered-варианты сохраняют ABI старых base-only функций. User root
 * проверяется fail-closed даже при use_user == 0; этот флаг отключает только
 * lookup. Валидный root без каталога ISO означает base-only. */
int32_t udlex_open_layered(
    const uint8_t *path, size_t path_len, const uint8_t *user_bundle,
    size_t user_bundle_len, uint8_t use_user, const udlex_options *options,
    const uint8_t *span_case, size_t span_case_len,
    const uint8_t *span_multiword, size_t span_multiword_len,
    udlex_lexicon **out, udlex_bytes *err);

int32_t udlex_load_layered(
    const uint8_t *bundle, size_t bundle_len, const uint8_t *language,
    size_t language_len, const uint8_t *user_bundle, size_t user_bundle_len,
    uint8_t use_user, const udlex_options *options, const uint8_t *span_case,
    size_t span_case_len, const uint8_t *span_multiword,
    size_t span_multiword_len, udlex_lexicon **out, udlex_bytes *err);

void udlex_close(udlex_lexicon *lexicon);

/* Открывает и проверяет таблицу именованных сущностей заранее: сама по себе
 * она читается лениво, при первом спановом или entity-вызове. */
int32_t udlex_ensure_entities(const udlex_lexicon *lexicon, udlex_bytes *err);
uint8_t udlex_entities_ready(const udlex_lexicon *lexicon);

int32_t udlex_meta_json(const udlex_lexicon *lexicon, udlex_bytes *out,
                        udlex_bytes *err);
int32_t udlex_languages_json(const uint8_t *bundle, size_t bundle_len,
                             udlex_bytes *out, udlex_bytes *err);

int32_t udlex_lemma(const udlex_lexicon *lexicon, const uint8_t *token,
                    size_t token_len, uint8_t flags, uint8_t *out,
                    size_t out_cap, size_t *out_len, udlex_source *out_source,
                    udlex_bytes *err);

int32_t udlex_knows(const udlex_lexicon *lexicon, const uint8_t *token,
                    size_t token_len, uint8_t flags, uint8_t *out,
                    udlex_bytes *err);

/* Пакет токенов за один переход границы.
 *
 * Вход: data — все токены подряд, offsets — count+1 смещений в data.
 * flags — NULL (первый токен открывает предложение) или count байт масок.
 *
 * Выход: изменившиеся леммы подряд в out_data, out_offsets — count+1 смещений
 * в нём. out_same — обязательные count транспортных флагов: 1 означает, что
 * лемма равна входному токену и вызывающий может вернуть свою строку без
 * копии. out_sources — NULL или count значений единственного provenance enum.
 * Равенство строки не смешивается с причиной результата. */
int32_t udlex_lemmatize(const udlex_lexicon *lexicon, const uint8_t *data,
                        size_t data_len, const uint32_t *offsets, size_t count,
                        const uint8_t *flags, uint8_t *out_data,
                        size_t out_cap, uint32_t *out_offsets,
                        uint8_t *out_same, udlex_source *out_sources,
                        size_t *out_len, udlex_bytes *err);

/* udlex_lemmatize с контекстом предложения. upos_offsets — NULL или count+1
 * смещений тегов UPOS в upos_data, по тегу на токен; пустой тег не
 * спрашивается. neighbours != 0 спрашивает ключи соседних слов; граница
 * предложения из flags для них — край. Точный ответ уступает ключу контекста
 * с источником UDLEX_SOURCE_CONTEXT. */
int32_t udlex_lemmatize_context(
    const udlex_lexicon *lexicon, const uint8_t *data, size_t data_len,
    const uint32_t *offsets, size_t count, const uint8_t *flags,
    const uint8_t *upos_data, size_t upos_len, const uint32_t *upos_offsets,
    uint8_t neighbours, uint8_t *out_data, size_t out_cap,
    uint32_t *out_offsets, uint8_t *out_same, udlex_source *out_sources,
    size_t *out_len, udlex_bytes *err);

int32_t udlex_lemma_span_text(const udlex_lexicon *lexicon, const uint8_t *text,
                              size_t text_len, uint8_t flags, uint8_t *out,
                              size_t out_cap, size_t *out_len,
                              udlex_source *out_source, udlex_bytes *err);

/* Спан, заданный токенами и зазорами между ними. has_separators == 0 означает
 * «зазоров нет»: каждый стык станет одним пробелом. Иначе sep_count обязан
 * равняться count - 1. */
int32_t udlex_lemma_span(const udlex_lexicon *lexicon, const uint8_t *data,
                         size_t data_len, const uint32_t *offsets, size_t count,
                         const uint8_t *sep_data, size_t sep_data_len,
                         const uint32_t *sep_offsets, size_t sep_count,
                         uint8_t has_separators, uint8_t flags, uint8_t *out,
                         size_t out_cap, size_t *out_len,
                         udlex_source *out_source,
                         udlex_bytes *err);

#ifdef __cplusplus
}
#endif

#endif /* UDLEX_H */
