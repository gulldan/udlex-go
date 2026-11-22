# udlex for Go

**English** · [Русский](README.ru.md) · [简体中文](README.zh-CN.md)

Local dictionary lemmatization for pretokenized text. The Go package links
a Rust core through cgo. [Python package and Rust core](https://github.com/gulldan/udlex)

## Install

Add the package to your project.

```sh
go get github.com/gulldan/udlex-go@v1.0.1
```

Use Go 1.24 or later, a C compiler and `CGO_ENABLED=1`. The module includes
native libraries for Linux and macOS on amd64 and arm64. On Alpine amd64,
build with `-tags musl`.

## Use

Pass a directory containing a compiled dictionary bundle and its ISO 639-3
language code. The example uses the small synthetic bundle in this checkout.
Run it with `go test -run '^Example$'`.

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

## Dictionaries

The [udlex 1.0.0 release](https://github.com/gulldan/udlex/releases/tag/v1.0.0)
publishes one archive per language, `udlex-dict-<iso>-1.0.0-bundle.tar.gz`.
Packages for noncommercial use end in `-nc`, for example English
`udlex-dict-eng-nc-1.0.0-bundle.tar.gz`. `DICTIONARIES.json` in the release
lists every language. Each archive unpacks into `bundle/<iso>`; unpack several
into the same directory.

```sh
curl -LO https://github.com/gulldan/udlex/releases/download/v1.0.0/udlex-dict-rus-1.0.0-bundle.tar.gz
tar -xzf udlex-dict-rus-1.0.0-bundle.tar.gz
```

Open it with `udlex.Load("bundle", "rus")`, or with `udlex.LoadInstalled("rus")`,
which reads the directory in `UDLEX_BUNDLE` or `./bundle`.

Load a trusted dictionary once and keep its files immutable. Reuse the
lexicon across goroutines and close it after all readers finish.

The native libraries can be rebuilt from the [Rust source](native/README.md).
Code is licensed under [MIT](LICENSE).

Dictionary packages include their own licenses and attribution in
`bundle/<iso>/LICENSES`. If you ship a dictionary with your
application, preserve that attribution and follow the dictionary's license.
Packages ending in `-nc` are for noncommercial use only.
