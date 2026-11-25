// Package websocket implements websocket-driver's native websocket_mask
// extension in pure Go.
package websocket

import (
	"encoding/binary"

	"github.com/oruby/oruby"
)

func init() {
	oruby.LazyGem("websocket_mask", initMask)
	oruby.GemAlias("websocket-driver/websocket_mask", "websocket_mask")
	oruby.GemAlias("websocket/mask", "websocket_mask")
	oruby.GemAlias("websocket/websocket_mask", "websocket_mask")
}

func initMask(mrb *oruby.MrbState) any {
	ws := mrb.DefineModule("WebSocket")
	maskMod := mrb.DefineModuleUnder(ws, "Mask")
	maskMod.DefineClassMethod("mask", maskPayload, mrb.ArgsReq(2))
	return nil
}

func maskPayload(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	payloadVal := args.Item(0)
	maskVal := args.Item(1)

	if maskVal.IsNil() {
		return payloadVal
	}
	if !maskVal.IsString() {
		return mrb.ETypeError().Raisef("no implicit conversion of %v into String", mrb.ClassOf(maskVal).Name())
	}
	mask := maskVal.Bytes()
	if len(mask) != 4 {
		return payloadVal
	}
	if !payloadVal.IsString() {
		return mrb.ETypeError().Raisef("no implicit conversion of %v into String", mrb.ClassOf(payloadVal).Name())
	}

	payload := payloadVal.Bytes()
	n := len(payload)
	if n == 0 {
		return mrb.Call(mrb.BytesValue(nil), "b")
	}

	out := make([]byte, n)
	m32 := binary.LittleEndian.Uint32(mask)
	m64 := uint64(m32) | (uint64(m32) << 32)

	i := 0
	for ; i+8 <= n; i += 8 {
		v := binary.LittleEndian.Uint64(payload[i:])
		binary.LittleEndian.PutUint64(out[i:], v^m64)
	}
	for ; i < n; i++ {
		out[i] = payload[i] ^ mask[i&3]
	}

	return mrb.Call(mrb.BytesValue(out), "b")
}
