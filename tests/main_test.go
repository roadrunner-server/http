package tests

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

// Size of the generated big-file fixtures. Static serving and x-sendfile both take
// a streaming path for files above a few KB, so the fixtures stay large.
const bigFixtureSize = 40528576

// Fixtures generated instead of tracked: 80 MB of blobs backing one length
// comparison (TestHTTPXSendFile) and one substring check (TestStaticBigFilePlugin).
var bigFixtures = []struct {
	path   string
	prefix string
	fill   byte
}{
	// Served by the static middleware; serveStaticSample asserts the body contains "sample".
	{path: "sample-big.txt", prefix: "sample\n", fill: 0},
	// Streamed via the X-Sendfile header; TestHTTPXSendFile compares the response
	// length against this file, so only its size matters.
	{path: "php_test_files/well", fill: 'R'},
}

func TestMain(m *testing.M) {
	for _, f := range bigFixtures {
		if err := writeBigFixture(f.path, f.prefix, f.fill); err != nil {
			fmt.Fprintf(os.Stderr, "generate %s: %v\n", f.path, err)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

// writeBigFixture creates path with prefix padded to bigFixtureSize by fill.
// An existing file of the right size is left alone so repeat runs skip the write.
func writeBigFixture(path, prefix string, fill byte) error {
	if st, err := os.Stat(path); err == nil && st.Size() == bigFixtureSize {
		return nil
	}

	content := append([]byte(prefix), bytes.Repeat([]byte{fill}, bigFixtureSize-len(prefix))...)

	return os.WriteFile(path, content, 0o600)
}
