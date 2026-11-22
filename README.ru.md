# udlex для Go

[English](README.md) · **Русский** · [简体中文](README.zh-CN.md)

Локальная словарная лемматизация для текста, разбитого на токены. Пакет Go
подключает ядро Rust через cgo. [Пакет Python и ядро Rust](https://github.com/gulldan/udlex)

## Установка

Добавьте пакет в проект.

```sh
go get github.com/gulldan/udlex-go@v1.0.1
```

Нужны Go 1.24 или новее, компилятор C и `CGO_ENABLED=1`. Модуль содержит
нативные библиотеки для Linux и macOS на amd64 и arm64. Для Alpine на amd64
используйте `-tags musl`.

## Быстрый старт

Передайте путь к набору скомпилированных словарей и код языка ISO 639-3.
Пример использует небольшой синтетический словарь из репозитория. Его можно
запустить командой `go test -run '^Example$'`.

```go
package main

import (
	"fmt"
	"log"

	"github.com/gulldan/udlex-go"
)

func main() {
	lexicon, err := udlex.Load("testdata/smoke-bundle", "eng")
	if err != nil {
		log.Fatal(err)
	}
	defer lexicon.Close()

	lemmas, err := lexicon.Lemmatize([]string{"cats", "mice", "unknown"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(lemmas) // [cat mouse unknown]
}
```

## Словари

[Выпуск udlex 1.0.0](https://github.com/gulldan/udlex/releases/tag/v1.0.0)
содержит по архиву на язык: `udlex-dict-<iso>-1.0.0-bundle.tar.gz`. Пакеты
только для некоммерческого использования оканчиваются на `-nc`, например
английский `udlex-dict-eng-nc-1.0.0-bundle.tar.gz`. Все языки перечислены в
`DICTIONARIES.json` того же выпуска. Архив распаковывается в `bundle/<iso>`;
несколько архивов можно распаковать в один каталог.

```sh
curl -LO https://github.com/gulldan/udlex/releases/download/v1.0.0/udlex-dict-rus-1.0.0-bundle.tar.gz
tar -xzf udlex-dict-rus-1.0.0-bundle.tar.gz
```

Откройте словарь через `udlex.Load("bundle", "rus")` или `udlex.LoadInstalled("rus")`:
он читает каталог из `UDLEX_BUNDLE` или `./bundle`.

Загружайте словарь из доверенного источника и сохраняйте его файлы неизменными,
пока он открыт. Один словарь можно использовать из нескольких горутин.
Вызовите `Close`, когда все чтения завершены.

Нативные библиотеки можно пересобрать [из исходников Rust](native/README.md).
Код распространяется по лицензии [MIT](LICENSE).

Пакеты словарей содержат свои лицензии и сведения об авторах в
`bundle/<iso>/LICENSES`. Если поставляете словарь вместе с
приложением, сохраните эти сведения и соблюдайте условия лицензии словаря.
Пакеты с суффиксом `-nc` предназначены только для некоммерческого использования.
