# Building the native libraries

The C ABI crate pins the [udlex engine](https://github.com/gulldan/udlex) to an
exact Git revision. Build from the repository root with the pinned Rust
toolchain and a linker for the target. On macOS set `MACOSX_DEPLOYMENT_TARGET=11.0`.

| Rust target | Destination under `lib/` |
| --- | --- |
| `aarch64-apple-darwin` | `darwin-arm64` |
| `x86_64-apple-darwin` | `darwin-amd64` |
| `aarch64-unknown-linux-gnu` | `linux-arm64` |
| `x86_64-unknown-linux-gnu` | `linux-amd64` |
| `x86_64-unknown-linux-musl` | `linux-musl-amd64` |

Set `target` to the Rust target and `platform` to its destination. Remap build
paths before compiling.

```sh
rustup target add "$target"
export CARGO_ENCODED_RUSTFLAGS
CARGO_ENCODED_RUSTFLAGS=$(printf '%s\037%s\037%s' \
  "--remap-path-prefix=$(pwd -P)=/udlex-go" \
  "--remap-path-prefix=${CARGO_HOME:-$HOME/.cargo}=/cargo" \
  "--remap-path-prefix=$(rustc --print sysroot)=/rust")
cargo build --locked --release --manifest-path native/Cargo.toml --target "$target"
cp "native/target/$target/release/libudlex_capi.a" "lib/$platform/"
```

Strip debug sections with the target's `strip` tool: `strip -S` on macOS,
`strip --strip-debug --remove-section=.llvmbc --remove-section=.llvmcmd` on
Linux. Go 1.27.4 and later refuse to link archives that keep the LLVM bitcode
sections. Regenerate the hashes from the repository root after rebuilding the
archives.

```sh
shasum -a 256 -- lib/*/libudlex_capi.a > lib/SHA256SUMS
```

CI checks the hashes and links each archive in the Go tests. For a release,
sign one `vX.Y.Z` tag matching the version in `native/Cargo.toml`.
