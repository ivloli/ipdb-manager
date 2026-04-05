package builder

import (
	"bufio"
	"fmt"
	"log"
	"net/netip"
	"os"
	"strings"
)

// SubnetEntry holds the representative CIDR for a province+ISP pair.
type SubnetEntry struct {
	Province string
	ISP      string
	CIDR     string
}

// BuildSubnetMap iterates the ip2region ipv4_source.txt file and returns a
// map of "province|isp" → representative CIDR subnet.
//
// File format (one record per line):
//
//	startIP|endIP|country|province|city|ISP|regionCode
//	Example: 1.0.8.0|1.0.15.255|中国|广东省|广州市|中国电信|CN
//
// Only mainland China records are included (country == "中国").
// Province and ISP names are normalized (see normalizeProvince / normalizeISP).
// The CIDR is computed from the actual IP range, not a fixed /24.
// The first segment encountered for each (province, ISP) pair is used.
func BuildSubnetMap(txtPath string) (map[string]string, error) {
	f, err := os.Open(txtPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", txtPath, err)
	}
	defer f.Close()

	firstIPMap := make(map[string][2]string) // key -> [startIP, endIP]
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	lineCount, validCount := 0, 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineCount++
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// startIP|endIP|country|province|city|ISP|regionCode
		parts := strings.Split(line, "|")
		if len(parts) < 7 {
			continue
		}

		country := strings.TrimSpace(parts[2])
		if country != "中国" {
			continue
		}
		validCount++

		province := strings.TrimSpace(parts[3])
		isp := strings.TrimSpace(parts[5])
		if province == "" || province == "0" || isp == "" || isp == "0" {
			continue
		}

		province = NormalizeProvince(province)
		isp = NormalizeISP(isp)

		startIP := strings.TrimSpace(parts[0])
		endIP := strings.TrimSpace(parts[1])
		if startIP == "" || endIP == "" {
			continue
		}

		key := province + "|" + isp
		if _, exists := firstIPMap[key]; !exists {
			firstIPMap[key] = [2]string{startIP, endIP}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", txtPath, err)
	}

	log.Printf("[builder] scanned %d lines, %d china records, %d province|isp keys",
		lineCount, validCount, len(firstIPMap))

	result := make(map[string]string, len(firstIPMap))
	for key, r := range firstIPMap {
		cidr, err := toCIDR(r[0], r[1])
		if err != nil {
			continue
		}
		result[key] = cidr
	}
	return result, nil
}

// NormalizeProvince strips trailing "省" / "市" so keys are consistent with
// what ip2region's SearchByStr also returns after normalization.
// "广东省" → "广东", "北京市" → "北京"
func NormalizeProvince(province string) string {
	province = strings.TrimSuffix(province, "省")
	province = strings.TrimSuffix(province, "市")
	return strings.TrimSpace(province)
}

// NormalizeISP strips "中国" and "云" so keys stay short and stable.
// "中国电信" → "电信", "中国联通" → "联通", "中国移动" → "移动"
func NormalizeISP(isp string) string {
	isp = strings.ReplaceAll(isp, "中国", "")
	isp = strings.ReplaceAll(isp, "云", "")
	return strings.TrimSpace(isp)
}

// toCIDR computes a converged CIDR block from a start/end IP pair.
// It supports both IPv4 and IPv6 source ranges.
func toCIDR(startIP, endIP string) (string, error) {
	start, err := netip.ParseAddr(startIP)
	if err != nil {
		return "", err
	}
	end, err := netip.ParseAddr(endIP)
	if err != nil {
		return "", err
	}
	if start.BitLen() != end.BitLen() {
		return "", fmt.Errorf("mixed ip versions: %s %s", startIP, endIP)
	}

	sb := start.AsSlice()
	eb := end.AsSlice()
	prefixLen := 0
	for i := 0; i < len(sb); i++ {
		if sb[i] == eb[i] {
			prefixLen += 8
			continue
		}
		x := sb[i] ^ eb[i]
		for bit := 7; bit >= 0; bit-- {
			if ((x >> uint(bit)) & 1) == 0 {
				prefixLen++
				continue
			}
			break
		}
		break
	}

	p, err := start.Prefix(prefixLen)
	if err != nil {
		return "", err
	}
	return p.Masked().String(), nil
}
