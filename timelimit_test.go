package gomupdf

import (
	"os"
	"strconv"
	"time"
)

// timeLimit turns a test's base limit, the figure that is comfortable on a fast
// machine, into the limit the test enforces. These assertions exist to catch
// quadratic or runaway work (the bugs they guard went from milliseconds to tens
// of seconds or minutes), so the limit is d times a factor: 10 by default, or
// $GOMUPDF_TEST_TIME_FACTOR, so a shared CI runner several times slower than a
// laptop still passes. A test whose old bug was only about 10x slower than the
// base figure passes maxFactor to keep the margin real; the environment
// variable still lowers it, and raises it only up to maxFactor.
func timeLimit(d time.Duration, maxFactor ...int) time.Duration {
	f := 10
	if n, err := strconv.Atoi(os.Getenv("GOMUPDF_TEST_TIME_FACTOR")); err == nil && n > 0 {
		f = n
	}
	if len(maxFactor) > 0 {
		f = min(f, maxFactor[0])
	}
	return d * time.Duration(f)
}
