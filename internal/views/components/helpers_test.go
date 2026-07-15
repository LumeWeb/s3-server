package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- JsEscape ---

func TestJsEscape_PlainString(t *testing.T) {
	assert.Equal(t, "hello world", JsEscape("hello world"))
}

func TestJsEscape_SingleQuote(t *testing.T) {
	assert.Equal(t, `\'`, JsEscape("'"))
}

func TestJsEscape_DoubleQuote(t *testing.T) {
	assert.Equal(t, `\"`, JsEscape("\""))
}

func TestJsEscape_Backslash(t *testing.T) {
	assert.Equal(t, `\\`, JsEscape(`\`))
}

func TestJsEscape_Newline(t *testing.T) {
	assert.Equal(t, `\n`, JsEscape("\n"))
}

func TestJsEscape_CarriageReturn(t *testing.T) {
	assert.Equal(t, `\r`, JsEscape("\r"))
}

func TestJsEscape_Tab(t *testing.T) {
	assert.Equal(t, `\t`, JsEscape("\t"))
}

func TestJsEscape_LessThan(t *testing.T) {
	// < is escaped to \x3c to prevent </script> injection
	assert.Equal(t, `\x3c`, JsEscape("<"))
}

func TestJsEscape_GreaterThan(t *testing.T) {
	assert.Equal(t, `\x3e`, JsEscape(">"))
}

func TestJsEscape_ComplexString(t *testing.T) {
	input := "user's \"data\" <script>\n"
	expected := `user\'s \"data\" \x3cscript\x3e\n`
	assert.Equal(t, expected, JsEscape(input))
}

func TestJsEscape_EmptyString(t *testing.T) {
	assert.Equal(t, "", JsEscape(""))
}

// --- MaskKey ---

func TestMaskKey_LongerThan4(t *testing.T) {
	const testKeyLong = "test-key-sample" // 15 chars
	result := MaskKey(testKeyLong)
	// Mask is len(key)-4 asterisks + last 4 chars = 11 + 4 = 15
	assert.Equal(t, "***********mple", result)
	assert.Len(t, result, len(testKeyLong))
	assert.True(t, result[len(result)-4:] == testKeyLong[len(testKeyLong)-4:])
}

func TestMaskKey_Exactly4(t *testing.T) {
	key := "ABCD"
	result := MaskKey(key)
	assert.Equal(t, "****", result)
}

func TestMaskKey_ShorterThan4(t *testing.T) {
	key := "AB"
	result := MaskKey(key)
	assert.Equal(t, "**", result)
}

func TestMaskKey_Empty(t *testing.T) {
	assert.Equal(t, "", MaskKey(""))
}

func TestMaskKey_SingleChar(t *testing.T) {
	assert.Equal(t, "*", MaskKey("A"))
}

func TestMaskKey_Last4Visible(t *testing.T) {
	key := "1234567890"
	result := MaskKey(key)
	assert.Equal(t, "******7890", result)
	assert.True(t, result[len(result)-4:] == "7890")
}

// --- HumanBytes ---

func TestHumanBytes_Zero(t *testing.T) {
	assert.Equal(t, "0 B", HumanBytes(0))
}

func TestHumanBytes_Bytes(t *testing.T) {
	assert.Equal(t, "500 B", HumanBytes(500))
}

func TestHumanBytes_Exactly1KB(t *testing.T) {
	assert.Equal(t, "1.00 KB", HumanBytes(1000))
}

func TestHumanBytes_Kilobytes(t *testing.T) {
	assert.Equal(t, "1.50 KB", HumanBytes(1500))
}

func TestHumanBytes_Megabytes(t *testing.T) {
	assert.Equal(t, "1.00 MB", HumanBytes(1_000_000))
}

func TestHumanBytes_Gigabytes(t *testing.T) {
	assert.Equal(t, "1.00 GB", HumanBytes(1_000_000_000))
}

func TestHumanBytes_Terabytes(t *testing.T) {
	assert.Equal(t, "1.00 TB", HumanBytes(1_000_000_000_000))
}

func TestHumanBytes_LargeValue(t *testing.T) {
	assert.Equal(t, "1.00 PB", HumanBytes(1_000_000_000_000_000))
}

func TestHumanBytes_SmallByteValue(t *testing.T) {
	assert.Equal(t, "1 B", HumanBytes(1))
}

func TestHumanBytes_999Bytes(t *testing.T) {
	assert.Equal(t, "999 B", HumanBytes(999))
}

func TestHumanBytes_OverFlowUsesLargestUnit(t *testing.T) {
	// Beyond EB range should clamp to EB
	// int64 max is ~9.2 * 10^18, which is ~9.2 EB
	result := HumanBytes(9_223_372_036_854_775_807)
	assert.Contains(t, result, "EB")
}

func TestHumanBytes_Negative(t *testing.T) {
	// Negative values: fb < k check fails (negative), Log of negative is NaN,
	// so i = floor(NaN) = NaN -> int(NaN) = 0 on most platforms, meaning "B".
	// Just verify it doesn't panic.
	_ = HumanBytes(-1)
}

// --- VersioningButtonLabel ---

func TestVersioningButtonLabel_Enabled(t *testing.T) {
	assert.Equal(t, "Suspend Versioning", VersioningButtonLabel("Enabled"))
}

func TestVersioningButtonLabel_Disabled(t *testing.T) {
	assert.Equal(t, "Enable Versioning", VersioningButtonLabel("Disabled"))
}

func TestVersioningButtonLabel_Empty(t *testing.T) {
	assert.Equal(t, "Enable Versioning", VersioningButtonLabel(""))
}

func TestVersioningButtonLabel_OtherValue(t *testing.T) {
	assert.Equal(t, "Enable Versioning", VersioningButtonLabel("Suspended"))
}

// --- CountWord ---

func TestCountWord_Singular(t *testing.T) {
	assert.Equal(t, "1 key", CountWord(1, "key"))
}

func TestCountWord_Plural(t *testing.T) {
	assert.Equal(t, "2 keys", CountWord(2, "key"))
}

func TestCountWord_Zero(t *testing.T) {
	assert.Equal(t, "0 keys", CountWord(0, "key"))
}

func TestCountWord_LargeNumber(t *testing.T) {
	assert.Equal(t, "100 buckets", CountWord(100, "bucket"))
}

func TestCountWord_IrregularPlural(t *testing.T) {
	// rickb777/plural handles common irregulars
	result := CountWord(2, "user")
	assert.Equal(t, "2 users", result)
}
