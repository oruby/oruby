package env

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"os"
	"strings"
	"testing"
)

func extractEnv(kv int) []string {
	env := os.Environ()
	ret := make([]string, len(env))
	for i, s := range env {
		switch kv {
		case -1:
			ret[i] = strings.SplitN(s, "=", 2)[0]
		case 0:
			ret[i] = s
		case 1:
			ret[i] = strings.SplitN(s, "=", 2)[1]
		default:
			panic("unsuporte value. expected -1, 0, 1")
		}
	}
	return ret
}

func Test_envValues(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("ENV.values")
	assert.NilError(t, err)

	values := []string{}
	err = mrb.Scan(ret, &values)
	assert.NilError(t, err)

	assert.Equal(t, values, extractEnv(1))
}

func Test_envHasValue(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	err := os.Setenv("ORUBY_TEST_KEY", "ORUBY_TEST")
	assert.NilError(t, err)

	ret, err := mrb.Eval("ENV.has_value? 'ORUBY_TEST'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)

	ret, err = mrb.Eval("ENV.has_value? 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_envKeys(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("ENV.keys")
	assert.NilError(t, err)

	values := []string{}
	err = mrb.Scan(ret, &values)
	assert.NilError(t, err)

	assert.Equal(t, values, extractEnv(-1))
}

func Test_envHasKey(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	err := os.Setenv("ORUBY_TEST_KEY", "ORUBY_TEST")
	assert.NilError(t, err)

	ret, err := mrb.Eval("ENV.has_key? 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)

	ret, err = mrb.Eval("ENV.has_key? 'NON_EXISTSNT_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_envIsEmpty(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("ENV.empty?")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	os.Clearenv()

	ret, err = mrb.Eval("ENV.empty?")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_envSize(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("ENV.size")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), len(os.Environ()))

	os.Clearenv()

	ret, err = mrb.Eval("ENV.size")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)
}

func Test_envToS(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("ENV.to_s")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), "ENV")
}

func Test_envClear(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	err := os.Setenv("ORUBY_TEST_KEY", "ORUBY_TEST")
	assert.NilError(t, err)

	ret, err := mrb.Eval("ENV.empty?")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	ret, err = mrb.Eval("ENV.clear")
	assert.NilError(t, err)

	ret, err = mrb.Eval("ENV.empty?")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_envDelete(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	err := os.Setenv("ORUBY_TEST_KEY", "ORUBY_TEST")
	assert.NilError(t, err)

	ret, err := mrb.Eval("ENV.has_key? 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)

	ret, err = mrb.Eval("ENV.__delete 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), "ORUBY_TEST")

	ret, err = mrb.Eval("ENV.has_key? 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_envGetKey(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	err := os.Setenv("ORUBY_TEST_KEY", "ORUBY_TEST")
	assert.NilError(t, err)

	ret, err := mrb.Eval("ENV['ORUBY_TEST_KEY']")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), "ORUBY_TEST")
}

func Test_envSetKey(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_= os.Unsetenv("ORUBY_TEST_KEY")

	ret, err := mrb.Eval("ENV.has_key? 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	ret, err = mrb.Eval("ENV['ORUBY_TEST_KEY'] = 'ORUBY_TEST'")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), "ORUBY_TEST")

	ret, err = mrb.Eval("ENV['ORUBY_TEST_KEY']")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), "ORUBY_TEST")

	ret, err = mrb.Eval("ENV['='] = 'ORUBY_TEST'")
	assert.Error(t, err, "Should reject keys with equal ('=')")

	ret, err = mrb.Eval("ENV[''] = 'ORUBY_TEST'")
	assert.Error(t, err, "Should reject empty keys")

	ret, err = mrb.Eval("ENV['NEW_VALUE'] = nil")
	assert.NilError(t, err)
	assert.Equal(t, ret.IsNil(), true)

	ret, err = mrb.Eval("ENV.has_key? 'NEW_VALUE'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	ret, err = mrb.Eval("ENV['ORUBY_TEST_KEY'] = nil")
	assert.NilError(t, err)
	assert.Equal(t, ret.IsNil(), true)

	ret, err = mrb.Eval("ENV.has_key? 'ORUBY_TEST_KEY'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

