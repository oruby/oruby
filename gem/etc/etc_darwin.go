package etc

/*
#include <pwd.h>
*/
import "C"

// pwExtra returns [change, uclass, expire] (BSD passwd fields)
func pwExtra(pw *C.struct_passwd) interface{} {
	return []interface{}{int(pw.pw_change), C.GoString(pw.pw_class), int(pw.pw_expire)}
}
