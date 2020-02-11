package time

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"testing"
	"time"
)

func TestTimeRb(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assert.AssertFile(t, mrb,"time_test.rb")
}

func TestSpaceShip(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assert.AssertCode(t, mrb, `
	assert('Time#<=>', '15.2.19.7.3') do
		t1 = Time.at(1300000000.0)
		t2 = Time.at(1400000000.0)
		t3 = Time.at(1500000000.0)

		assert_equal(1, t2 <=> t1)
		assert_equal(0, t2 <=> t2)
		assert_equal(-1, t2 <=> t3)
		assert_nil(t2 <=> nil)
	end`)
}

func TestGetGM(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assert.AssertCode(t, mrb, `
	assert('Time#getgm', '15.2.19.7.8') do
		t = Time.at(1300000000.0)
		gm = t.getgm
  		assert_equal("Sun Mar 13 07:06:40 2011", gm.asctime)
  		assert_equal("Sun Mar 13 07:06:40 2011", Time.at(1300000000.0).getgm.asctime); 
	end
	`)
}

func TestToS(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	assert.AssertCode(t, mrb, `
	assert('Time#to_s') do
		assert_equal("2003-04-05 06:07:08 UTC", Time.gm(2003,4,5,6,7,8,9).to_s)
	end
	`)
}

func TestIsTimeDST(t *testing.T) {
	dst := time.Date(2012, time.December, 23, 0,0,0,0,time.UTC)
	assert.Expect(t, !isDST(dst), "%v should not be dst", dst)

	dst = time.Date(2012, time.July, 23, 0,0,0,0,time.Local)
	assert.Expect(t, isDST(dst), "%v should be dst", dst)
}
