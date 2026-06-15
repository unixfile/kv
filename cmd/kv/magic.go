package main

import "fmt"

// runMagic prints a file(1) magic block that identifies keyval files by
// the hash line kv writes. Print-only, like completion: the user merges
// it into ~/.magic and removes it by the fence markers. file(1) honors
// ~/.magic across Unix (Linux, macOS, the BSDs); the block is additive,
// so a host without file loses nothing.
func runMagic() int {
	fmt.Print(magicBlock)
	return 0
}

// magicBlock matches the first-line hash line and reports the keyval
// MIME, extension and spec URL, so `file foo.kv` prints the URL. The
// fence comments delimit a managed block for clean removal; the URL
// makes the fence collision-proof in a shared ~/.magic, at the cost of
// a non-/ sed delimiter in the uninstall comment. The regex also
// accepts the line riding a shebang (#!... # <url>).
const magicBlock = "# >>> github.com/unixfile/keyval >>>\n" +
	"# file(1) magic for keyval\n" +
	"# install:   kv magic >> ~/.magic\n" +
	"# uninstall: sed -i '\\|^# >>> github.com/unixfile/keyval >>>$|,\\|^# <<< github.com/unixfile/keyval <<<$|d' ~/.magic\n" +
	"0\tstring\t#\n" +
	">0\tregex/1l\t(^|[[:space:]])#[[:space:]]+github[.]com/unixfile/keyval([[:space:]]|$)\tkeyval data, github.com/unixfile/keyval\n" +
	"!:mime\ttext/x-keyval\n" +
	"!:ext\tkv\n" +
	"# <<< github.com/unixfile/keyval <<<\n"
