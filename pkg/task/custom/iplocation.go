package custom

import (
	"github.com/hanc00l/nemo_go/pkg/conf"
	"github.com/hanc00l/nemo_go/pkg/logging"
	"github.com/hanc00l/nemo_go/pkg/utils"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Target string `json:"target"`
	OrgId  *int   `json:"orgId"`
}

type IpLocation struct {
	customMap  map[string]string
	customBMap map[string]string
	customCMap map[string]string
}

// NewIPLocation 创建iplocation对象
func NewIPLocation() *IpLocation {
	ipl := &IpLocation{
		customMap:  make(map[string]string),
		customBMap: make(map[string]string),
		customCMap: make(map[string]string),
	}
	ipl.loadCustomIP()
	ipl.loadQQwry()
	return ipl
}

// FindPublicIP 查询纯真数据库获取公网IP归属地
func (ipl *IpLocation) FindPublicIP(ip string) string {
	qqWry := NewQQwry()
	result := qqWry.Find(ip)

	return result.Country
}

// FindCustomIP 查询自定义IP归属地
func (ipl *IpLocation) FindCustomIP(ip string) string {
	result, ok := ipl.customMap[ip]
	if ok {
		return result
	}
	ipBytes := strings.Split(ip, ".")
	if len(ipBytes) != 4 {
		return ""
	}
	result, ok = ipl.customCMap[strings.Join([]string{ipBytes[0], ipBytes[1], ipBytes[2], "0"}, ".")]
	if ok {
		return result
	}
	result, ok = ipl.customBMap[strings.Join([]string{ipBytes[0], ipBytes[1], "0", "0"}, ".")]
	if ok {
		return result
	}

	return ""
}

// loadQQwry 加载纯真IP数据库
func (ipl *IpLocation) loadQQwry() {
	IPData.FilePath = filepath.Join(conf.GetRootPath(), "thirdparty/qqwry/qqwry.dat")
	res := IPData.InitIPData()

	if v, ok := res.(error); ok {
		logging.RuntimeLog.Error(v)
		logging.CLILog.Error(v)
	} else {
		//logging.RuntimeLog.Infof("纯真IP库加载完成,共加载:%d 条 Domain 记录", IPData.IPNum)
	}
}

// loadCustomIP 加载自定义IP归属地库
func (ipl *IpLocation) loadCustomIP() {
	content, err := os.ReadFile(filepath.Join(conf.GetRootPath(), "thirdparty/custom/iplocation-custom-B.txt"))
	if err != nil {
		logging.RuntimeLog.Error(err)
		logging.CLILog.Error(err)
	} else {
		for _, line := range strings.Split(string(content), "\n") {
			txt := strings.TrimSpace(line)
			if txt == "" || strings.HasPrefix(txt, "#") {
				continue
			}
			ipLocationArrays := strings.Split(txt, " ")
			if len(ipLocationArrays) < 2 {
				continue
			}
			ips := strings.Split(ipLocationArrays[0], ".")
			if len(ips) != 4 {
				continue
			}
			ipl.customBMap[strings.Join([]string{ips[0], ips[1], "0", "0"}, ".")] = ipLocationArrays[1]
		}
	}

	content, err = os.ReadFile(filepath.Join(conf.GetRootPath(), "thirdparty/custom/iplocation-custom-C.txt"))
	if err != nil {
		logging.RuntimeLog.Error(err)
		logging.CLILog.Error(err)
	} else {
		for _, line := range strings.Split(string(content), "\n") {
			txt := strings.TrimSpace(line)
			if txt == "" || strings.HasPrefix(txt, "#") {
				continue
			}
			ipLocationArrays := strings.Split(txt, " ")
			if len(ipLocationArrays) < 2 {
				continue
			}
			ips := strings.Split(ipLocationArrays[0], ".")
			if len(ips) != 4 {
				continue
			}
			ipl.customCMap[strings.Join([]string{ips[0], ips[1], ips[2], "0"}, ".")] = ipLocationArrays[1]
		}
	}

	content, err = os.ReadFile(filepath.Join(conf.GetRootPath(), "thirdparty/custom/iplocation-custom.txt"))
	if err != nil {
		logging.RuntimeLog.Error(err)
		logging.CLILog.Error(err)
	} else {
		for _, line := range strings.Split(string(content), "\n") {
			txt := strings.TrimSpace(line)
			if txt == "" || strings.HasPrefix(txt, "#") {
				continue
			}
			ipLocationArrays := strings.Split(txt, " ")
			if len(ipLocationArrays) < 2 {
				continue
			}
			ips := utils.ParseIP(ipLocationArrays[0])
			for _, ip := range ips {
				ipl.customMap[ip] = ipLocationArrays[1]
			}
		}
	}
}
