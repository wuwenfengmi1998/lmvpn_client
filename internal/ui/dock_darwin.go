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

static void setDockIconVisible(int visible) {
	Class cls = objc_getClass("NSApplication");
	id app = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("sharedApplication"));
	((void (*)(id, SEL, long))objc_msgSend)(app, sel_getUid("setActivationPolicy:"), visible ? 0 : 1);
}

static void cmActivateApp(void) {
	Class cls = objc_getClass("NSApplication");
	id app = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("sharedApplication"));
	((void (*)(id, SEL, BOOL))objc_msgSend)(app, sel_getUid("activateIgnoringOtherApps:"), YES);
}

// cmShowAndActivate switches to regular activation policy (dock icon
// visible), activates the app, and makes every NSWindow key+front.
// This bypasses GLFW's glfwShowWindow (which uses orderFront:nil and
// is unreliable during NSApplication activation-policy transitions).
static void cmShowAndActivate(void) {
	Class cls = objc_getClass("NSApplication");
	id app = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("sharedApplication"));
	// Regular activation policy (dock icon visible).
	((void (*)(id, SEL, long))objc_msgSend)(app, sel_getUid("setActivationPolicy:"), 0);
	// Bring app to foreground.
	((void (*)(id, SEL, BOOL))objc_msgSend)(app, sel_getUid("activateIgnoringOtherApps:"), YES);
	// Make every window key and visible.  NSArray* windows = [app windows];
	id windows = ((id (*)(id, SEL))objc_msgSend)(app, sel_getUid("windows"));
	// NSUInteger count = [windows count];
	unsigned long count = ((unsigned long (*)(id, SEL))objc_msgSend)(windows, sel_getUid("count"));
	for (unsigned long i = 0; i < count; i++) {
		// id w = [windows objectAtIndex:i];
		id w = ((id (*)(id, SEL, unsigned long))objc_msgSend)(windows, sel_getUid("objectAtIndex:"), i);
		// [w makeKeyAndOrderFront:nil];
		((void (*)(id, SEL, id))objc_msgSend)(w, sel_getUid("makeKeyAndOrderFront:"), nil);
	}
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

func showDockIcon() { C.setDockIconVisible(1) }
func hideDockIcon() { C.setDockIconVisible(0) }

func activateApp() { C.cmActivateApp() }

func showAndActivate() { C.cmShowAndActivate() }

func registerReopenHandler() { C.cmRegisterReopenHandler() }
