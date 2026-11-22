# udlex 的 Go 绑定

[English](README.md) · [Русский](README.ru.md) · **简体中文**

为已分词的文本提供本地词典式词形还原。Go 包通过 cgo 链接 Rust 核心。
[Python 包和 Rust 核心](https://github.com/gulldan/udlex)

## 安装

运行以下命令将包添加到项目。

```sh
go get github.com/gulldan/udlex-go@v1.0.1
```

需要 Go 1.24 或更高版本、C 编译器以及 `CGO_ENABLED=1`。模块包含 Linux 和 macOS
上 amd64 与 arm64 的原生库。在 Alpine amd64 上构建时，请使用 `-tags musl`。

## 快速开始

传入编译好的词典包目录和 ISO 639-3 语言代码。示例使用仓库里的小型合成词典，
可用 `go test -run '^Example$'` 运行。

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

## 词典

[udlex 1.0.0 发布](https://github.com/gulldan/udlex/releases/tag/v1.0.0)为每种语言提供一个归档
`udlex-dict-<iso>-1.0.0-bundle.tar.gz`。仅限非商业使用的包以 `-nc` 结尾，例如英语
`udlex-dict-eng-nc-1.0.0-bundle.tar.gz`。同一发布中的 `DICTIONARIES.json` 列出全部语言。
每个归档解压到 `bundle/<iso>`，多个归档可以解压到同一目录。

```sh
curl -LO https://github.com/gulldan/udlex/releases/download/v1.0.0/udlex-dict-rus-1.0.0-bundle.tar.gz
tar -xzf udlex-dict-rus-1.0.0-bundle.tar.gz
```

用 `udlex.Load("bundle", "rus")` 打开，或用 `udlex.LoadInstalled("rus")`：它读取
`UDLEX_BUNDLE` 指定的目录或 `./bundle`。

请从可信来源加载词典，在打开期间保持文件不变。同一个词典可供多个 goroutine
使用。等所有读取完成后再调用 `Close`。

可以用 [Rust 源代码](native/README.md) 重新构建原生库。代码采用 [MIT 许可证](LICENSE)。

词典包在 `bundle/<iso>/LICENSES` 中随附各自的许可证和署名信息。
如果随应用一起分发词典，请保留署名信息并遵守该词典的许可证。带有 `-nc` 后缀的包仅供非商业使用。
