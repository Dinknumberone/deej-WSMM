package deej

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	wca "github.com/moutend/go-wca"
	"go.uber.org/zap"
)

// setDefaultAudioRenderDeviceByFriendlyName sets the Windows default playback device
// (for console/multimedia/communications roles) by matching an active render endpoint's
// PKEY_Device_FriendlyName.
func setDefaultAudioRenderDeviceByFriendlyName(logger *zap.SugaredLogger, friendlyName string) error {
	if strings.TrimSpace(friendlyName) == "" {
		return errors.New("empty device name")
	}

	endpointID, err := findActiveRenderEndpointIDByFriendlyName(friendlyName)
	if err != nil {
		return err
	}

	return setDefaultEndpointAllRoles(endpointID)
}

func toggleDefaultAudioRenderDeviceByFriendlyNames(logger *zap.SugaredLogger, firstFriendlyName, secondFriendlyName string) error {
	firstID, err := findActiveRenderEndpointIDByFriendlyName(firstFriendlyName)
	if err != nil {
		return err
	}

	secondID, err := findActiveRenderEndpointIDByFriendlyName(secondFriendlyName)
	if err != nil {
		return err
	}

	currentID, err := getDefaultRenderEndpointID()
	if err != nil {
		return err
	}

	var targetID string
	switch strings.ToLower(strings.TrimSpace(currentID)) {
	case strings.ToLower(firstID):
		targetID = secondID
	case strings.ToLower(secondID):
		targetID = firstID
	default:
		// Unknown current default device; prefer the first as a deterministic fallback.
		targetID = firstID
	}

	logger.Infow("Switching default playback device", "from", currentID, "to", targetID)
	return setDefaultEndpointAllRoles(targetID)
}

func findActiveRenderEndpointIDByFriendlyName(friendlyName string) (string, error) {
	// go-wca/COM calls must happen on a single OS thread (apartment-threaded COM).
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := coInitialize(); err != nil {
		return "", err
	}
	defer ole.CoUninitialize()

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator,
		0,
		wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator,
		&enumerator,
	); err != nil {
		return "", fmt.Errorf("create device enumerator: %w", err)
	}
	defer enumerator.Release()

	var deviceCollection *wca.IMMDeviceCollection
	if err := enumerator.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &deviceCollection); err != nil {
		return "", fmt.Errorf("enumerate audio endpoints: %w", err)
	}
	defer deviceCollection.Release()

	var deviceCount uint32
	if err := deviceCollection.GetCount(&deviceCount); err != nil {
		return "", fmt.Errorf("get device count: %w", err)
	}

	for deviceIdx := uint32(0); deviceIdx < deviceCount; deviceIdx++ {
		var endpoint *wca.IMMDevice
		if err := deviceCollection.Item(deviceIdx, &endpoint); err != nil {
			return "", fmt.Errorf("get device %d: %w", deviceIdx, err)
		}

		match, endpointID, err := endpointMatchesFriendlyName(endpoint, friendlyName)
		endpoint.Release()
		if err != nil {
			return "", err
		}
		if match {
			return endpointID, nil
		}
	}

	return "", fmt.Errorf("no active render device found with friendly name %q", friendlyName)
}

func endpointMatchesFriendlyName(endpoint *wca.IMMDevice, desiredFriendlyName string) (bool, string, error) {
	var propertyStore *wca.IPropertyStore
	if err := endpoint.OpenPropertyStore(wca.STGM_READ, &propertyStore); err != nil {
		return false, "", fmt.Errorf("open property store: %w", err)
	}
	defer propertyStore.Release()

	value := &wca.PROPVARIANT{}
	if err := propertyStore.GetValue(&wca.PKEY_Device_FriendlyName, value); err != nil {
		return false, "", fmt.Errorf("get friendly name property: %w", err)
	}

	actualFriendlyName := strings.TrimSpace(value.String())
	if !strings.EqualFold(actualFriendlyName, strings.TrimSpace(desiredFriendlyName)) {
		return false, "", nil
	}

	var endpointID string
	if err := endpoint.GetId(&endpointID); err != nil {
		return false, "", fmt.Errorf("get endpoint id: %w", err)
	}
	return true, endpointID, nil
}

func getDefaultRenderEndpointID() (string, error) {
	// go-wca/COM calls must happen on a single OS thread (apartment-threaded COM).
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := coInitialize(); err != nil {
		return "", err
	}
	defer ole.CoUninitialize()

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator,
		0,
		wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator,
		&enumerator,
	); err != nil {
		return "", fmt.Errorf("create device enumerator: %w", err)
	}
	defer enumerator.Release()

	var endpoint *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &endpoint); err != nil {
		return "", fmt.Errorf("get default audio endpoint: %w", err)
	}
	defer endpoint.Release()

	var endpointID string
	if err := endpoint.GetId(&endpointID); err != nil {
		return "", fmt.Errorf("get default endpoint id: %w", err)
	}
	return endpointID, nil
}

func coInitialize() error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		// 0x00000001 corresponds to S_FALSE: COM already initialized on this thread.
		const sFalse = 1
		oleError := &ole.OleError{}
		if errors.As(err, &oleError) && oleError.Code() == sFalse {
			return nil
		}

		return fmt.Errorf("CoInitializeEx: %w", err)
	}

	return nil
}

// --- PolicyConfig (undocumented) plumbing ---

// These GUIDs/ABI are widely used in practice to set the default endpoint.
// We attempt a couple of known IIDs for compatibility across Windows versions.
var (
	clsidPolicyConfigClient = ole.NewGUID("{870af99c-171d-4f9e-af0d-e63df40c2bc9}")

	iidPolicyConfig        = ole.NewGUID("{f8679f50-850a-41cf-9c72-430f290290c8}")
	iidPolicyConfigVista   = ole.NewGUID("{568b9108-44bf-40b4-9006-86afe5b5a620}")
	iidPolicyConfigUnknown = ole.NewGUID("{294935ce-f637-4e7c-a41b-ab255460b862}")
)

func setDefaultEndpointAllRoles(endpointID string) error {
	// PolicyConfig is a COM object; keep calls on a single OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := coInitialize(); err != nil {
		return err
	}
	defer ole.CoUninitialize()

	policyUnknown, setDefaultEndpoint, err := createPolicyConfig()
	if err != nil {
		return err
	}
	defer policyUnknown.Release()

	endpointIDUTF16, err := syscall.UTF16PtrFromString(endpointID)
	if err != nil {
		return fmt.Errorf("convert endpoint id: %w", err)
	}

	// Apply to all roles for best UX.
	for _, role := range []uint32{0, 1, 2} {
		hr, _, _ := syscall.Syscall(
			setDefaultEndpoint,
			3,
			uintptr(unsafe.Pointer(policyUnknown)),
			uintptr(unsafe.Pointer(endpointIDUTF16)),
			uintptr(role),
		)
		if hr != 0 {
			return fmt.Errorf("SetDefaultEndpoint role=%d failed: HRESULT 0x%X", role, uint32(hr))
		}
	}

	return nil
}

func createPolicyConfig() (*ole.IUnknown, uintptr, error) {
	tryIIDs := []*ole.GUID{iidPolicyConfig, iidPolicyConfigVista, iidPolicyConfigUnknown}

	for _, iid := range tryIIDs {
		unknown, err := ole.CreateInstance(clsidPolicyConfigClient, iid)
		if err != nil {
			continue
		}

		if unknown == nil || unknown.RawVTable == nil {
			if unknown != nil {
				unknown.Release()
			}
			continue
		}

		// SetDefaultEndpoint is at vtable index 12 for the commonly used PolicyConfig variants.
		// Guard against a nil proc pointer to avoid crashing on syscall.
		vtbl := (*[13]uintptr)(unsafe.Pointer(unknown.RawVTable))
		setDefaultEndpoint := vtbl[12]
		if setDefaultEndpoint == 0 {
			unknown.Release()
			continue
		}

		return unknown, setDefaultEndpoint, nil
	}

	return nil, 0, errors.New("failed to create PolicyConfigClient (no supported IPolicyConfig IID)")
}
