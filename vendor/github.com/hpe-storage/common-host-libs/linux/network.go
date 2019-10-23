// Copyright 2019 Hewlett Packard Enterprise Development LP.

package linux

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
)

var (
	ipcommand              = "ip"
	nameMtuStateKeyPattern = "^\\d+:\\s+(?P<Name>.*):\\s+<.*mtu\\s+(?P<Mtu>\\d+)\\s+.*state\\s+(?P<UP>\\w+)"
	ipv4AddrBcastPattern   = "inet\\s+(?P<IPAddress>[\\d\\.]*)/(?P<Mask>\\d+)\\s+brd\\s+(?P<Bcast>[\\d\\.]*)\\s+"
	etherAddrKeyPattern    = "ether\\s+(?P<Mac>[\\d\\:A-Fa-f]+)"
	ipv4AddrKeyPattern     = "inet\\s+(?P<IPAddress>[\\d\\.]*)/(?P<Mask>\\d+)\\s+"
	up                     = "UP"
	unknown                = "UNKNOWN"
	ethtool                = "ethtool"
	maskFmt                = "%d.%d.%d.%d"
	linkStatusPattern      = "\\s+Link detected:\\s+yes"
)

//GetHostname : get the hostname for the host
func getHostname() (string, error) {
	host, err := os.Hostname()
	return host, err
}

//GetHostNameAndDomain : get host name and domain
func GetHostNameAndDomain() ([]string, error) {
	log.Trace(">>>>> GetHostNameAndDomain")
	defer log.Trace("<<<<< GetHostNameAndDomain")

	hostname, err := getHostname()
	if err != nil {
		return nil, err
	}
	domainname, err := getDomainName()
	if err != nil {
		return []string{hostname, ""}, nil
	}
	log.Tracef("hostname=%s domain=%s", hostname, domainname)
	return []string{hostname, domainname}, nil
}

//GetDomainName : of the host
func getDomainName() (string, error) {
	var addr string
	interfaces, err := getInterfacesIPAddr()
	if err != nil {
		return "", err
	}
	for _, i := range interfaces {
		if i.AddressV4 != "" {
			addrs, err := net.LookupAddr(i.AddressV4)
			if err != nil {
				return "", err
			}
			if len(addrs) == 0 {
				return "", fmt.Errorf("no domain found")
			}
			addr = addrs[0]
			break
		}
	}
	return addr, nil
}

// GetIPV4NetworkAddress returns network address for given ipv4 address and netmask
func GetIPV4NetworkAddress(ipv4Address, netMask string) (networkAddress string, err error) {
	log.Trace("GetIPV4NetworkAddress called with ", ipv4Address, " mask ", netMask)
	if ipv4Address == "" || netMask == "" {
		return "", fmt.Errorf("invalid ipv4 address or mask provided to get network address")
	}

	var networkOctets [4]string
	ipOctets := strings.Split(ipv4Address, ".")
	maskOctets := strings.Split(netMask, ".")

	if len(ipOctets) != 4 || len(maskOctets) != 4 {
		return "", fmt.Errorf("invalid ipv4 address or mask provided to get network address")
	}

	for index := range ipOctets {
		ipOctet, err := strconv.ParseUint(ipOctets[index], 10, 16)
		if err != nil {
			return "", fmt.Errorf("unable to parse ip address. Error:  %s", err.Error())
		}
		maskOctet, err := strconv.ParseUint(maskOctets[index], 10, 16)
		if err != nil {
			return "", fmt.Errorf("unable to parse network mask %s", err.Error())
		}
		if ipOctet > 255 || maskOctet > 255 {
			return "", fmt.Errorf("invalid ipv4 address or mask provided to get network address")
		}
		networkOctet := ipOctet & maskOctet
		networkOctets[index] = strconv.FormatUint(networkOctet, 10)
	}
	networkAddress = fmt.Sprintf("%s.%s.%s.%s", networkOctets[0], networkOctets[1], networkOctets[2], networkOctets[3])
	log.Trace("network address being returned ", networkAddress)
	return networkAddress, nil
}

//GetNetworkInterfaces : get the array of network interfaces
func GetNetworkInterfaces() ([]*model.Network, error) {
	log.Trace(">>>>> GetNetworkInterfaces")
	defer log.Trace("<<<<< GetNetworkInterfaces")

	interfaces, err := getInterfacesIPAddr()
	return interfaces, err
}

func getMaskString(intMask int) string {
	log.Trace(">>>>> getMaskString called with ", intMask)
	defer log.Trace("<<<<< getMaskString")

	var mask uint64
	mask = (0xFFFFFFFF << (32 - uint64(intMask))) & 0xFFFFFFFF //intMask is for eg: /24
	var localmask []uint64
	dmask := uint64(32)
	localmask = make([]uint64, 0, 4)
	for i := 1; i <= 4; i++ {
		tmp := mask >> (dmask - 8) & 0xFF
		localmask = append(localmask, uint64(tmp))
		dmask -= 8
	}
	maskV4 := fmt.Sprintf(maskFmt, localmask[0], localmask[1], localmask[2], localmask[3])
	log.Tracef("mask(v4): %s", maskV4)

	return maskV4
}

func getInterfacesIPAddr() ([]*model.Network, error) {
	log.Trace(">>>>> getInterfacesIpAddr")
	defer log.Trace("<<<<< getInterfacesIPAddr")

	var nics []*model.Network
	var nic *model.Network
	args := []string{"addr"}
	out, _, err := util.ExecCommandOutput(ipcommand, args)
	if err != nil {
		return nil, err
	}
	outArr := strings.Split(out, "\n")
	for _, line := range outArr {
		log.Trace("line :", line)
		r := regexp.MustCompile(nameMtuStateKeyPattern)
		if r.MatchString(line) {
			matchedMap := util.FindStringSubmatchMap(line, r)
			if nic != nil {
				nics = append(nics, nic)
				log.Trace("Added :", nic.Name)
			}
			mtu, er := strconv.ParseInt(matchedMap["Mtu"], 10, 32)
			if er != nil {
				log.Trace("Err :", err)
				return nics, er
			}
			if matchedMap["UP"] == up {
				nic = &model.Network{Name: matchedMap["Name"], Mtu: mtu, Up: true}
			} else if matchedMap["UP"] == unknown {
				// ip addr and ip link shows state as UNKNOWN with old kernel versions(/sys/class/net/docker0/operstate)
				// https://access.redhat.com/solutions/1443363
				// obtain using ethtool
				status := getInterfaceStatus(matchedMap["Name"])
				nic = &model.Network{Name: matchedMap["Name"], Mtu: mtu, Up: status}
			} else {
				nic = &model.Network{Name: matchedMap["Name"], Mtu: mtu, Up: false}
			}
		} else {
			if nic != nil {
				nic, err = matchIPPattern(line, nic)
			}
		}
	}
	if nic != nil {
		nics = append(nics, nic)
		log.Tracef("getInterfacesIpAddr added %v to slice of NICs", nic)
	}
	return nics, err
}

// obtain interface status using ethtool
func getInterfaceStatus(name string) bool {
	args := []string{name}
	out, _, err := util.ExecCommandOutput(ethtool, args)
	if err != nil {
		return false
	}
	log.Traceln("Obtained link status using ethtool for", name)
	r := regexp.MustCompile(linkStatusPattern)
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if r.MatchString(line) {
			return true
		}
	}
	return false
}

func matchIPPattern(line string, nic *model.Network) (*model.Network, error) {
	log.Tracef(">>>> matchIPPattern called with %s", line)
	defer log.Trace("<<<<< matchIPPattern")

	r := regexp.MustCompile(ipv4AddrKeyPattern)
	if r.MatchString(line) {
		matchedMap := util.FindStringSubmatchMap(line, r)
		log.Trace("matched out Map:", matchedMap)

		mask, er := strconv.ParseInt(matchedMap["Mask"], 10, 64)
		if er != nil {
			return nic, er
		}
		nic.AddressV4 = matchedMap["IPAddress"]
		nic.MaskV4 = getMaskString(int(mask))

	} else {
		r := regexp.MustCompile(etherAddrKeyPattern)
		if r.MatchString(line) {
			matchedMap := util.FindStringSubmatchMap(line, r)

			log.Trace("matched out map:", matchedMap)
			nic.Mac = matchedMap["Mac"]

		} else {
			r := regexp.MustCompile(ipv4AddrBcastPattern)
			if r.MatchString(line) {
				matchedMap := util.FindStringSubmatchMap(line, r)

				log.Trace("matched out map:", matchedMap)
				mask, er := strconv.ParseInt(matchedMap["Mask"], 10, 64)
				if er != nil {
					return nic, er
				}
				nic.BroadcastV4 = matchedMap["Bcast"]
				nic.AddressV4 = matchedMap["Address"]
				nic.MaskV4 = getMaskString(int(mask))
			}
		}
	}

	log.Tracef("matchIPPattern returning %v", nic)
	return nic, nil
}
