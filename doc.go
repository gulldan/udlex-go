// Package udlex looks up lemmas for pretokenized text using local dictionaries.
//
// Open a compiled bundle with [Load] and reuse the resulting [Lexicon] across
// goroutines. Close it after all readers finish. The Rust core is linked
// statically through cgo.
package udlex
