//go:build !nomupdf

package gomupdf

// The globals gomupdf_bigstack.h declares: the stack size of the threads
// that run deeply recursive MuPDF calls, and the flag that says the current
// thread is one.
//
// 64 MiB is address space, committed only as the recursion touches it.
// MuPDF's save needs about 81 bytes of stack per indirect reference hop
// (measured on Alpine 3.23 arm64, MuPDF 1.26.11, a Garbage 1 save of a /Next
// chain of outline items: 1 MiB held 13,100 items and 4 MiB 52,100), so this
// holds a chain of 800,000 sibling outline items (900,000 crashed), far
// beyond any ordinary file, and about 40 concurrent calls fit in the corpus
// test's 3 GiB RLIMIT_AS (256 MiB, before, fitted about ten). Past such a
// limit the thread cannot be created and the call fails with an error.

/*
#include <stddef.h>
size_t gomupdf_bigstack_size = (size_t)64 << 20;
__thread int gomupdf_on_bigstack;
__thread int gomupdf_overflows;
*/
import "C"

// setBigStackSize sets the stack size of the threads gomupdf_bigstack.h
// creates and returns the previous one. Tests use it to measure the stack
// MuPDF needs and to make thread creation fail; it must not
// run concurrently with MuPDF calls.
func setBigStackSize(n uint64) uint64 {
	old := uint64(C.gomupdf_bigstack_size)
	C.gomupdf_bigstack_size = C.size_t(n)
	return old
}
