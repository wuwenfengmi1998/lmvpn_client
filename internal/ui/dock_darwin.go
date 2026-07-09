//go:build darwin

package ui

/*
#cgo LDFLAGS: -framework Cocoa
#include <objc/objc.h>
#include <objc/runtime.h>
#include <objc/message.h>

// Forward declaration: Go callback (defined via //export in
// dock_callback_darwin.go).  Called when the user re-opens the .app
// bundle while the process is still running (e.g. after closing the
// window to tray).
extern void cmAppBecameActive(void);

// appShouldHandleReopen is the IMP added to GLFWApplicationDelegate
// for the applicationShouldHandleReopen:hasVisibleWindows: selector.
// This is the delegate method macOS calls when the user re-opens an
// already-running .app bundle.  We return YES so macOS proceeds with
// its default activation, and invoke the Go callback to show the
// hidden window.
static BOOL appShouldHandleReopen(id self, SEL cmd, id sender, BOOL flag) {
    cmAppBecameActive();
    return YES;
}

static void cmActivateApp(void) {
	Class cls = objc_getClass("NSApplication");
	id app = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("sharedApplication"));
	((void (*)(id, SEL, BOOL))objc_msgSend)(app, sel_getUid("activateIgnoringOtherApps:"), YES);
}

// cmRegisterReopenHandler adds applicationShouldHandleReopen:
// hasVisibleWindows: to the GLFWApplicationDelegate class at runtime
// (class_addMethod).  This lets us detect when macOS re-opens the
// .app bundle without starting a new process (which bypasses our IPC
// single-instance mechanism).  If the class already implements the
// method, this is a no-op.
static void cmRegisterReopenHandler(void) {
	Class cls = objc_getClass("GLFWApplicationDelegate");
	if (!cls) return;
	SEL sel = sel_getUid("applicationShouldHandleReopen:hasVisibleWindows:");
	if (class_getInstanceMethod(cls, sel)) return;
	class_addMethod(cls, sel, (IMP)appShouldHandleReopen, "B@:@B");
}
*/
import "C"

func activateApp() { C.cmActivateApp() }

func registerReopenHandler() { C.cmRegisterReopenHandler() }
