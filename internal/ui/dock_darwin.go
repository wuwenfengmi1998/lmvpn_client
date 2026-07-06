//go:build darwin

package ui

/*
#cgo LDFLAGS: -framework Cocoa
#include <objc/objc.h>
#include <objc/runtime.h>
#include <objc/message.h>

static void setDockIconVisible(int visible) {
	Class cls = objc_getClass("NSApplication");
	id app = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("sharedApplication"));
	((void (*)(id, SEL, long))objc_msgSend)(app, sel_getUid("setActivationPolicy:"), visible ? 0 : 1);
}
*/
import "C"

func showDockIcon() { C.setDockIconVisible(1) }
func hideDockIcon() { C.setDockIconVisible(0) }
