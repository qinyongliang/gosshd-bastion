//go:build windows

package agent

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const windowsBootSessionKey = `SOFTWARE\GosshdBootSession`

var regCreateVolatileKey = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegCreateKeyExW")

func readSystemBootID() (string, error) {
	stamp, err := windowsSystemProcessStart()
	if err != nil {
		return "", err
	}
	id, err := windowsBootSessionID(registry.LOCAL_MACHINE, windowsBootSessionKey, stamp)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// The installed LocalSystem service initializes the machine-wide key. A
		// foreground user without that privilege can still survive process restarts
		// using its own volatile hive. Instance guarding and stale-peer probing also
		// protect account changes; neither fallback is written to agent.json.
		return windowsBootSessionID(registry.CURRENT_USER, windowsBootSessionKey, stamp)
	}
	return id, err
}

// A BCD boot-loader GUID can survive reboots and disk cloning; it is not a boot
// session ID. Use an in-memory volatile key with a freshly generated UUID. The
// kernel System process creation time additionally invalidates stale caches.
func windowsBootSessionID(root registry.Key, path string, stamp uint64) (string, error) {
	if stamp == 0 {
		return "", errors.New("kernel boot stamp is missing")
	}
	read := func() (string, bool) {
		key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
		if err != nil {
			return "", false
		}
		defer key.Close()
		marker, _, err := key.GetIntegerValue("KernelStart")
		if err != nil || marker != stamp {
			return "", false
		}
		value, _, err := key.GetStringValue("BootID")
		parsed, parseErr := uuid.Parse(value)
		return value, err == nil && parseErr == nil && parsed != uuid.Nil
	}
	if id, ok := read(); ok {
		return id, nil
	}
	// Avoid legacy 32-bit registry virtualization creating a private HKLM
	// shadow. Unelevated users may read the service's machine-wide ID, but only
	// an elevated/service token initializes it; otherwise use the user hive.
	if root == registry.LOCAL_MACHINE && !windows.GetCurrentProcessToken().IsElevated() {
		return "", windows.ERROR_ACCESS_DENIED
	}
	lockScope := fmt.Sprintf("%x/%s", uint64(root), path)
	if root == registry.CURRENT_USER {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return "", err
		}
		lockScope += "/" + user.User.Sid.String()
	}
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`Global\gosshd-boot-init-%x`, sha256.Sum256([]byte(lockScope))))
	mutex, err := windows.CreateMutexEx(nil, name, 0, windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return "", err
	}
	defer windows.CloseHandle(mutex)
	// Mutex ownership is thread-affine; pin only this short initialization.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result, err := windows.WaitForSingleObject(mutex, 5000)
	if err != nil {
		return "", err
	}
	if result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED {
		return "", errors.New("timed out initializing volatile boot identity")
	}
	defer windows.ReleaseMutex(mutex)
	if id, ok := read(); ok {
		return id, nil
	}
	keyPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var handle windows.Handle
	var disposition uint32
	const regOptionVolatile = 1
	status, _, _ := regCreateVolatileKey.Call(uintptr(root), uintptr(unsafe.Pointer(keyPath)), 0, 0, regOptionVolatile, uintptr(registry.QUERY_VALUE|registry.SET_VALUE|registry.WOW64_64KEY), 0, uintptr(unsafe.Pointer(&handle)), uintptr(unsafe.Pointer(&disposition)))
	if status != 0 {
		return "", fmt.Errorf("create volatile boot identity: %w", windows.Errno(status))
	}
	key := registry.Key(handle)
	defer key.Close()
	id := uuid.NewString()
	if err := key.SetStringValue("BootID", id); err != nil {
		return "", err
	}
	// Publish the validating stamp last, so lock-free readers cannot accept an
	// old UUID while a cache from a different boot is being refreshed.
	if err := key.SetQWordValue("KernelStart", stamp); err != nil {
		return "", err
	}
	return id, nil
}

func windowsSystemProcessStart() (uint64, error) {
	size := uint32(64 * 1024)
	for attempts := 0; attempts < 8 && size <= 16*1024*1024; attempts++ {
		buffer := make([]byte, size)
		var returned uint32
		err := windows.NtQuerySystemInformation(windows.SystemProcessInformation, unsafe.Pointer(&buffer[0]), size, &returned)
		if errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			size = max(size*2, returned+4096)
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("query kernel boot stamp: %w", err)
		}
		if returned > size {
			return 0, errors.New("invalid system process information size")
		}
		const minimum = uint32(unsafe.Offsetof(windows.SYSTEM_PROCESS_INFORMATION{}.UniqueProcessID) + unsafe.Sizeof(uintptr(0)))
		for offset := uint32(0); offset <= returned && returned-offset >= minimum; {
			info := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buffer[offset]))
			if info.UniqueProcessID == 4 && info.CreateTime > 0 {
				return uint64(info.CreateTime), nil
			}
			next := info.NextEntryOffset
			if next == 0 {
				break
			}
			if next < minimum || next > returned-offset {
				return 0, errors.New("invalid system process information offset")
			}
			offset += next
		}
		return 0, errors.New("kernel System process creation time is unavailable")
	}
	return 0, errors.New("system process information exceeds boot-query limit")
}
