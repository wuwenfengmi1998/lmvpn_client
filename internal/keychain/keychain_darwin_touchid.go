//go:build darwin

package keychain

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security -framework LocalAuthentication -framework Foundation

#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <objc/objc.h>
#include <objc/runtime.h>
#include <objc/message.h>

// LAPolicyDeviceOwnerAuthenticationWithBiometrics = 1

// biometricAvailable returns 1 if Touch ID is available, 0 otherwise.
// Uses the Objective-C runtime to call LAContext without including
// Objective-C headers (which cannot be parsed by the C compiler).
static int biometricAvailable(void) {
	Class cls = objc_getClass("LAContext");
	if (!cls) return 0;

	id alloc = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("alloc"));
	if (!alloc) return 0;
	id context = ((id (*)(id, SEL))objc_msgSend)(alloc, sel_getUid("init"));
	if (!context) {
		((void (*)(id, SEL))objc_msgSend)(alloc, sel_getUid("release"));
		return 0;
	}

	// canEvaluatePolicy:error: returns BOOL, takes (NSInteger, NSError**)
	// LAPolicyDeviceOwnerAuthenticationWithBiometrics = 1
	BOOL result = ((BOOL (*)(id, SEL, long, void *))objc_msgSend)(
		context, sel_getUid("canEvaluatePolicy:error:"), 1, NULL);

	((void (*)(id, SEL))objc_msgSend)(context, sel_getUid("release"));
	return result ? 1 : 0;
}

// secAccessControlCreateBiometric creates a SecAccessControlRef with
// kSecAccessControlBiometryAny. Must be released with CFRelease by caller.
static SecAccessControlRef secAccessControlCreateBiometric(void) {
	SecAccessControlRef acl = SecAccessControlCreateWithFlags(
		kCFAllocatorDefault,
		kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly,
		kSecAccessControlBiometryAny,
		NULL);
	return acl;
}

// storeBiometricItem stores data in the keychain with Touch ID protection.
// Returns 0 on success, or a non-zero OSStatus error code on failure.
static int storeBiometricItem(CFStringRef service, CFStringRef account, CFDataRef data) {
	SecAccessControlRef acl = secAccessControlCreateBiometric();
	if (!acl) {
		return errSecAllocate;
	}

	const void *keys[] = {
		kSecClass,
		kSecAttrService,
		kSecAttrAccount,
		kSecValueData,
		kSecAttrAccessControl,
	};
	const void *values[] = {
		kSecClassGenericPassword,
		service,
		account,
		data,
		acl,
	};
	CFDictionaryRef query = CFDictionaryCreate(
		kCFAllocatorDefault,
		keys, values, sizeof(keys) / sizeof(keys[0]),
		&kCFTypeDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks);

	// Delete any existing item first (Add fails on duplicates).
	SecItemDelete(query);

	OSStatus status = SecItemAdd(query, NULL);
	CFRelease(query);
	CFRelease(acl);
	return (int)status;
}

// getBiometricItem retrieves data from the keychain, prompting for Touch ID
// via an LAContext with a localized reason. Returns:
//   0 on success (dataOut filled, caller must CFRelease)
//  -1 if item not found
//  -128 (errSecUserCanceled) if user canceled
//   other positive OSStatus error codes on failure
//
// prompt is the localized Touch ID prompt string (CFStringRef).
static int getBiometricItem(CFStringRef service, CFStringRef account, CFStringRef prompt, CFDataRef *dataOut) {
	// Create an LAContext and set its localizedReason for the Touch ID
	// prompt. We use the objc runtime to avoid including Objective-C
	// headers. CFStringRef is toll-free bridged to NSString*.
	id laContext = NULL;
	if (prompt) {
		Class cls = objc_getClass("LAContext");
		if (cls) {
			id alloc = ((id (*)(Class, SEL))objc_msgSend)(cls, sel_getUid("alloc"));
			if (alloc) {
				laContext = ((id (*)(id, SEL))objc_msgSend)(alloc, sel_getUid("init"));
			}
		}
		if (laContext) {
			((void (*)(id, SEL, id))objc_msgSend)(
				laContext, sel_getUid("setLocalizedReason:"), (id)prompt);
		}
	}

	CFDictionaryRef query;
	if (laContext) {
		const void *keys[] = {
			kSecClass,
			kSecAttrService,
			kSecAttrAccount,
			kSecMatchLimit,
			kSecReturnData,
			kSecUseAuthenticationContext,
		};
		const void *values[] = {
			kSecClassGenericPassword,
			service,
			account,
			kSecMatchLimitOne,
			kCFBooleanTrue,
			laContext,
		};
		query = CFDictionaryCreate(
			kCFAllocatorDefault,
			keys, values, sizeof(keys) / sizeof(keys[0]),
			&kCFTypeDictionaryKeyCallBacks,
			&kCFTypeDictionaryValueCallBacks);
	} else {
		// Fallback: no LAContext (e.g. LAContext class not available).
		const void *keys[] = {
			kSecClass,
			kSecAttrService,
			kSecAttrAccount,
			kSecMatchLimit,
			kSecReturnData,
		};
		const void *values[] = {
			kSecClassGenericPassword,
			service,
			account,
			kSecMatchLimitOne,
			kCFBooleanTrue,
		};
		query = CFDictionaryCreate(
			kCFAllocatorDefault,
			keys, values, sizeof(keys) / sizeof(keys[0]),
			&kCFTypeDictionaryKeyCallBacks,
			&kCFTypeDictionaryValueCallBacks);
	}

	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(query, &result);
	CFRelease(query);

	if (laContext) {
		((void (*)(id, SEL))objc_msgSend)(laContext, sel_getUid("release"));
	}

	if (status == errSecItemNotFound) {
		return -1;
	}
	if (status != errSecSuccess) {
		return (int)status;
	}
	*dataOut = (CFDataRef)result;
	return 0;
}

// deleteBiometricItem deletes a keychain item by service+account.
// Returns 0 on success (including "not found"), or OSStatus on error.
static int deleteBiometricItem(CFStringRef service, CFStringRef account) {
	const void *keys[] = {
		kSecClass,
		kSecAttrService,
		kSecAttrAccount,
	};
	const void *values[] = {
		kSecClassGenericPassword,
		service,
		account,
	};
	CFDictionaryRef query = CFDictionaryCreate(
		kCFAllocatorDefault,
		keys, values, sizeof(keys) / sizeof(keys[0]),
		&kCFTypeDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks);

	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	if (status == errSecItemNotFound) {
		return 0;
	}
	return (int)status;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

// errSecUserCanceled is the macOS Security framework error code returned
// when the user cancels a Touch ID / password prompt (-128).
const errSecUserCanceled = -128

// errSecMissingEntitlementCode is errSecMissingEntitlement (-34018),
// returned by the Data Protection Keychain when the app is not properly
// code-signed with keychain-access-groups entitlement.
const errSecMissingEntitlementCode = -34018

var (
	biometricCacheOnce sync.Once
	biometricCacheVal  bool
)

// biometricAvailable reports whether Touch ID is available on this Mac.
// The result is computed once and cached.
func biometricAvailable() bool {
	biometricCacheOnce.Do(func() {
		biometricCacheVal = C.biometricAvailable() == 1
	})
	return biometricCacheVal
}

// cfRelease releases a CFTypeRef (CFString, CFData, etc.).
func cfRelease(ref C.CFTypeRef) {
	if ref != 0 {
		C.CFRelease(ref)
	}
}

// cfString creates a CFStringRef from a Go string. The caller must
// CFRelease the result.
func cfString(s string) (C.CFStringRef, error) {
	if s == "" {
		return 0, nil
	}
	cs := C.CFStringCreateWithBytes(
		C.kCFAllocatorDefault,
		(*C.UInt8)(unsafe.Pointer(&[]byte(s)[0])),
		C.CFIndex(len(s)),
		C.kCFStringEncodingUTF8,
		C.false)
	if cs == 0 {
		return 0, fmt.Errorf("CFStringCreateWithBytes failed for %q", s)
	}
	return cs, nil
}

// cfData creates a CFDataRef from a byte slice. The caller must
// CFRelease the result.
func cfData(b []byte) (C.CFDataRef, error) {
	var p *C.UInt8
	if len(b) > 0 {
		p = (*C.UInt8)(unsafe.Pointer(&b[0]))
	}
	cd := C.CFDataCreate(C.kCFAllocatorDefault, p, C.CFIndex(len(b)))
	if cd == 0 {
		return 0, fmt.Errorf("CFDataCreate failed")
	}
	return cd, nil
}

// storeBiometricItemGo stores a secret in the keychain with Touch ID
// protection. It first deletes any existing item (which may or may not
// have biometric protection), then adds a new biometric-protected item.
func storeBiometricItemGo(service, account, secret string) error {
	svcRef, err := cfString(service)
	if err != nil {
		return fmt.Errorf("keychain biometric store %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(svcRef))

	accRef, err := cfString(account)
	if err != nil {
		return fmt.Errorf("keychain biometric store %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(accRef))

	secretBytes := []byte(secret)
	dataRef, err := cfData(secretBytes)
	if err != nil {
		return fmt.Errorf("keychain biometric store %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(dataRef))

	status := C.storeBiometricItem(svcRef, accRef, dataRef)
	if status != 0 {
		if status == errSecMissingEntitlementCode {
			return fmt.Errorf("keychain biometric store %s: %w", account, ErrSecMissingEntitlement)
		}
		return fmt.Errorf("keychain biometric store %s: OSStatus %d", account, status)
	}
	return nil
}

// getBiometricItemGo retrieves a secret from the keychain, prompting
// for Touch ID if the item is biometric-protected. prompt is the
// localized text shown in the Touch ID dialog.
func getBiometricItemGo(service, account, prompt string) (string, error) {
	svcRef, err := cfString(service)
	if err != nil {
		return "", fmt.Errorf("keychain biometric get %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(svcRef))

	accRef, err := cfString(account)
	if err != nil {
		return "", fmt.Errorf("keychain biometric get %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(accRef))

	promptRef, err := cfString(prompt)
	if err != nil {
		return "", fmt.Errorf("keychain biometric get %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(promptRef))

	var dataOut C.CFDataRef
	status := C.getBiometricItem(svcRef, accRef, promptRef, &dataOut)
	if status == -1 {
		return "", ErrNotFound
	}
	if status == errSecUserCanceled {
		return "", ErrUserCanceled
	}
	if status != 0 {
		if status == errSecMissingEntitlementCode {
			return "", fmt.Errorf("keychain biometric get %s: %w", account, ErrSecMissingEntitlement)
		}
		return "", fmt.Errorf("keychain biometric get %s: OSStatus %d", account, status)
	}
	defer cfRelease(C.CFTypeRef(dataOut))

	bytes := C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(dataOut)), C.int(C.CFDataGetLength(dataOut)))
	return string(bytes), nil
}

// deleteBiometricItemGo deletes a keychain item by service+account.
// It works regardless of whether the item has biometric protection.
func deleteBiometricItemGo(service, account string) error {
	svcRef, err := cfString(service)
	if err != nil {
		return fmt.Errorf("keychain biometric delete %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(svcRef))

	accRef, err := cfString(account)
	if err != nil {
		return fmt.Errorf("keychain biometric delete %s: %w", account, err)
	}
	defer cfRelease(C.CFTypeRef(accRef))

	status := C.deleteBiometricItem(svcRef, accRef)
	if status != 0 {
		if status == errSecMissingEntitlementCode {
			return fmt.Errorf("keychain biometric delete %s: %w", account, ErrSecMissingEntitlement)
		}
		return fmt.Errorf("keychain biometric delete %s: OSStatus %d", account, status)
	}
	return nil
}
