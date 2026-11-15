package etc

/*
#include <pwd.h>
*/
import "C"

func pwExtra(pw *C.struct_passwd) interface{} { return nil }
