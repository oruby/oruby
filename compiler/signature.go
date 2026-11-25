package compiler

import (
	"fmt"
	"strings"
)

// methodSignature derives, from the irep of a compiled method (or block), the
// argument spec (the operand of its OP_ENTER, 0 when it has none) and the
// parameter list that Method#parameters shows, as "kind:name;kind:name".
// It follows go-mrb.c's orb_params, which does the same for bytecode methods,
// so a compiled method answers as its bytecode would.
func methodSignature(u *IRepUnit, strict bool) (aspec uint32, params string) {
	if len(u.Instructions) == 0 || u.Instructions[0].Op != OpENTER {
		return 0, ""
	}
	aspec = u.Instructions[0].A
	m1, o, r := aspecReq(aspec), aspecOpt(aspec), aspecRest(aspec)
	m2, k, kd := aspecPost(aspec), aspecKey(aspec), aspecKdict(aspec)
	lv := func(i int) string {
		if i >= 0 && i < len(u.Locals) {
			return u.Locals[i]
		}
		return ""
	}
	var out []string
	pair := func(kind, name string) {
		// a hidden local (prism.go ORB_BLOCK_PARAMS) is named by MRI as none
		if name == "" || strings.HasPrefix(name, "__oruby_bp") {
			out = append(out, kind+":")
			return
		}
		out = append(out, kind+":"+name)
	}
	req := "opt"
	if strict {
		req = "req"
	}
	reg := 0
	for i := 0; i < m1; i, reg = i+1, reg+1 {
		pair(req, lv(reg))
	}
	for i := 0; i < o; i, reg = i+1, reg+1 {
		pair("opt", lv(reg))
	}
	if r != 0 {
		if lv(reg) != "__oruby_implicit_rest" {
			pair("rest", lv(reg))
		}
		reg++
	}
	for i := 0; i < m2; i, reg = i+1, reg+1 {
		pair(req, lv(reg))
	}
	kdname := ""
	if k > 0 || kd != 0 {
		kdname = lv(reg)
		reg++
	}
	blkname := lv(reg + 1)
	if k > 0 {
		var keyp []string
		var opts []string
		found := 0
		for _, ins := range u.Instructions[1:] {
			if found >= k {
				break
			}
			name := ""
			if int(ins.B) < len(u.Syms) {
				name = u.Syms[ins.B]
			}
			switch ins.Op {
			case OpKEY_P:
				keyp = append(keyp, name)
			case OpKARG:
				isOpt := false
				for _, kp := range keyp {
					if kp == name {
						isOpt = true
					}
				}
				if isOpt {
					opts = append(opts, "key:"+name) // MRI lists the required keywords first
				} else {
					out = append(out, "keyreq:"+name)
				}
				found++
			}
		}
		out = append(out, opts...)
	}
	if kd != 0 {
		if kdname == "**nil" {
			out = append(out, "nokey:")
		} else {
			pair("keyrest", kdname)
		}
	}
	if aspec&1 != 0 {
		pair("block", blkname)
	}
	if (aspec>>23)&1 != 0 {
		out = append(out, "noblock:")
	}
	return aspec, strings.Join(out, ";")
}

// signatureLiteral is the Go expression passing the signature of u to the
// runtime when a compiled method is defined.
func signatureLiteral(u *IRepUnit, strict bool) string {
	aspec, params := methodSignature(u, strict)
	return fmt.Sprintf("oruby.AotSig{Aspec: 0x%x, KeyReq: %v, Params: %q, Known: true}", aspec, strings.Contains(params, "keyreq:"), params)
}
