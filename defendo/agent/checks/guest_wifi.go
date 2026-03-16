//go:build windows

package checks

import (
	"fmt"
	"net"
	"strings"
	"syscall"
	"unsafe"

	"github.com/yusufpapurcu/wmi"
)

var (
	wlanapi            = syscall.NewLazyDLL("wlanapi.dll")
	wlanOpenHandle     = wlanapi.NewProc("WlanOpenHandle")
	wlanCloseHandle    = wlanapi.NewProc("WlanCloseHandle")
	wlanEnumInterfaces = wlanapi.NewProc("WlanEnumInterfaces")
	wlanQueryInterface = wlanapi.NewProc("WlanQueryInterface")
	wlanFreeMemory     = wlanapi.NewProc("WlanFreeMemory")
)

const wlanIntfOpcodeCurrentConnection = 7

// Windows WLAN API structures.
// https://learn.microsoft.com/en-us/windows/win32/api/wlanapi/

type dot11SSID struct {
	SSIDLength uint32
	SSID       [32]byte
}

type wlanAssociationAttributes struct {
	Dot11Ssid         dot11SSID
	Dot11BssType      uint32
	Dot11Bssid        [6]byte
	Dot11PhyType      uint32
	UDot11PhyIndex    uint32
	WlanSignalQuality uint32
	UlRxRate          uint32
	UlTxRate          uint32
}

type wlanSecurityAttributes struct {
	BSecurityEnabled     int32
	BOneXEnabled         int32
	Dot11AuthAlgorithm   uint32
	Dot11CipherAlgorithm uint32
}

type wlanConnectionAttributes struct {
	IsState              uint32
	WlanConnectionMode   uint32
	StrProfileName       [256]uint16
	WlanAssociationAttrs wlanAssociationAttributes
	WlanSecurityAttrs    wlanSecurityAttributes
}

type wlanInterfaceInfo struct {
	InterfaceGUID        [16]byte
	InterfaceDescription [256]uint16
	IsState              uint32
}

type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
}

// networkAdapterConfig maps to Win32_NetworkAdapterConfiguration.
type networkAdapterConfig struct {
	Index     uint32
	IPAddress []string
	IPSubnet  []string
}

// networkAdapter maps to Win32_NetworkAdapter.
type networkAdapter struct {
	Index       uint32
	Name        string
	AdapterType string
	GUID        string
}

// wirelessInterface holds SSID + IP info for a Wi-Fi adapter.
type wirelessInterface struct {
	SSID    string
	GUID    string
	Subnets []string // CIDR notation
}

// GuestWiFiCheck verifies that guest Wi-Fi networks are on a separate subnet.
type GuestWiFiCheck struct{}

func (c GuestWiFiCheck) Name() string {
	return "Guest Wi-Fi Subnet Separation"
}

func (c GuestWiFiCheck) Run() Result {
	wireless, err := getWirelessInterfaces()
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("WLAN enumeration failed: %v", err)}
	}
	if len(wireless) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "No wireless interfaces found."}
	}

	// Collect subnets for non-guest (wired/primary) adapters via WMI.
	var allConfigs []networkAdapterConfig
	q := wmi.CreateQuery(&allConfigs, "WHERE IPEnabled = True")
	if err := wmi.Query(q, &allConfigs); err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("WMI network query failed: %v", err)}
	}

	// Build a set of all subnets from wired (non-wireless) adapters.
	var adapters []networkAdapter
	q2 := wmi.CreateQuery(&adapters, "WHERE NetEnabled = True")
	_ = wmi.Query(q2, &adapters) // best-effort

	wirelessGUIDs := make(map[string]bool)
	for _, w := range wireless {
		wirelessGUIDs[strings.ToUpper(w.GUID)] = true
	}

	wiredSubnets := make([]string, 0)
	for _, cfg := range allConfigs {
		// Find the adapter for this config to determine if it's wired.
		isWireless := false
		for _, a := range adapters {
			if a.Index == cfg.Index && wirelessGUIDs[strings.ToUpper(a.GUID)] {
				isWireless = true
				break
			}
		}
		if !isWireless {
			wiredSubnets = append(wiredSubnets, toCIDRList(cfg.IPAddress, cfg.IPSubnet)...)
		}
	}

	// Match wireless interfaces to their IP config.
	for i := range wireless {
		for _, a := range adapters {
			if strings.EqualFold(a.GUID, wireless[i].GUID) {
				for _, cfg := range allConfigs {
					if cfg.Index == a.Index {
						wireless[i].Subnets = toCIDRList(cfg.IPAddress, cfg.IPSubnet)
					}
				}
			}
		}
	}

	// Identify guest Wi-Fi interfaces (SSID contains "guest").
	type wifiResult struct {
		SSID     string `json:"ssid"`
		Subnets  []string `json:"subnets"`
		Isolated bool   `json:"isolated"`
	}

	var results []wifiResult
	violations := 0

	for _, w := range wireless {
		isGuest := strings.Contains(strings.ToLower(w.SSID), "guest")
		if !isGuest {
			continue
		}
		isolated := !sharesSubnet(w.Subnets, wiredSubnets)
		results = append(results, wifiResult{SSID: w.SSID, Subnets: w.Subnets, Isolated: isolated})
		if !isolated {
			violations++
		}
	}

	if len(results) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("%d wireless interface(s) found, none identified as guest networks.", len(wireless)),
			Value:   wireless,
		}
	}

	if violations > 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: fmt.Sprintf("%d guest Wi-Fi network(s) share a subnet with wired/primary interfaces.", violations),
			Value:   results,
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d guest Wi-Fi network(s) are on separate subnets.", len(results)),
		Value:   results,
	}
}

// getWirelessInterfaces returns connected Wi-Fi interfaces with their SSID and GUID.
func getWirelessInterfaces() ([]wirelessInterface, error) {
	var clientHandle syscall.Handle
	var negotiatedVersion uint32

	ret, _, _ := wlanOpenHandle.Call(
		2, // client version
		0,
		uintptr(unsafe.Pointer(&negotiatedVersion)),
		uintptr(unsafe.Pointer(&clientHandle)),
	)
	if ret != 0 {
		return nil, fmt.Errorf("WlanOpenHandle error code %d", ret)
	}
	defer wlanCloseHandle.Call(uintptr(clientHandle), 0)

	var ifaceList *wlanInterfaceInfoList
	ret, _, _ = wlanEnumInterfaces.Call(
		uintptr(clientHandle),
		0,
		uintptr(unsafe.Pointer(&ifaceList)),
	)
	if ret != 0 {
		return nil, fmt.Errorf("WlanEnumInterfaces error code %d", ret)
	}
	defer wlanFreeMemory.Call(uintptr(unsafe.Pointer(ifaceList)))

	count := ifaceList.NumberOfItems
	if count == 0 {
		return nil, nil
	}

	// The interface info array starts right after the list header.
	firstEntry := unsafe.Pointer(uintptr(unsafe.Pointer(ifaceList)) + unsafe.Sizeof(*ifaceList))
	ifaceSize := unsafe.Sizeof(wlanInterfaceInfo{})

	var interfaces []wirelessInterface
	for i := uint32(0); i < count; i++ {
		info := (*wlanInterfaceInfo)(unsafe.Pointer(uintptr(firstEntry) + uintptr(i)*ifaceSize))
		guid := guidToString(info.InterfaceGUID)

		var connAttrs *wlanConnectionAttributes
		var dataSize uint32
		ret, _, _ = wlanQueryInterface.Call(
			uintptr(clientHandle),
			uintptr(unsafe.Pointer(info)),
			wlanIntfOpcodeCurrentConnection,
			0,
			uintptr(unsafe.Pointer(&dataSize)),
			uintptr(unsafe.Pointer(&connAttrs)),
			0,
		)
		if ret != 0 {
			// Interface not connected — skip.
			continue
		}
		defer wlanFreeMemory.Call(uintptr(unsafe.Pointer(connAttrs)))

		ssidLen := connAttrs.WlanAssociationAttrs.Dot11Ssid.SSIDLength
		ssid := string(connAttrs.WlanAssociationAttrs.Dot11Ssid.SSID[:ssidLen])

		interfaces = append(interfaces, wirelessInterface{SSID: ssid, GUID: guid})
	}

	return interfaces, nil
}

// guidToString converts a raw 16-byte GUID to a {xxxxxxxx-xxxx-...} string.
func guidToString(g [16]byte) string {
	return fmt.Sprintf("{%08X-%04X-%04X-%04X-%012X}",
		uint32(g[0])|uint32(g[1])<<8|uint32(g[2])<<16|uint32(g[3])<<24,
		uint16(g[4])|uint16(g[5])<<8,
		uint16(g[6])|uint16(g[7])<<8,
		uint16(g[8])|uint16(g[9])<<8,
		g[10:16],
	)
}

// toCIDRList pairs each IP address with its subnet mask and returns CIDR strings.
func toCIDRList(ips, masks []string) []string {
	cidrs := make([]string, 0, len(ips))
	for i, ip := range ips {
		if i >= len(masks) {
			break
		}
		parsed := net.ParseIP(ip)
		mask := net.IPMask(net.ParseIP(masks[i]).To4())
		if parsed == nil || mask == nil {
			continue
		}
		ones, _ := mask.Size()
		cidrs = append(cidrs, fmt.Sprintf("%s/%d", parsed.Mask(mask).String(), ones))
	}
	return cidrs
}

// sharesSubnet returns true if any subnet in 'a' also appears in 'b'.
func sharesSubnet(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}