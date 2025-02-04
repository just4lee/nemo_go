package workerapi

import (
	"errors"
	"fmt"
	"github.com/hanc00l/nemo_go/pkg/comm"
	"github.com/hanc00l/nemo_go/pkg/conf"
	"github.com/hanc00l/nemo_go/pkg/logging"
	"github.com/hanc00l/nemo_go/pkg/task/domainscan"
	"github.com/hanc00l/nemo_go/pkg/task/onlineapi"
	"github.com/hanc00l/nemo_go/pkg/task/pocscan"
	"github.com/hanc00l/nemo_go/pkg/task/portscan"
	"github.com/hanc00l/nemo_go/pkg/utils"
	"github.com/remeh/sizedwaitgroup"
	"strings"
	"sync"
)

type XScanConfig struct {
	OrgId       *int `json:"orgid,omitempty"`
	WorkspaceId int  `json:"workspaceId"`
	// orgscan
	IsOrgIP     bool   `json:"orgip,omitempty"`     //XOrganizaiton：IP资产
	IsOrgDomain bool   `json:"orgdomain,omitempty"` //XOrganizaiton：domain资产
	OrgIPPort   string `json:"orgport,omitempty"`   // Org扫描时，是否指定IP的端口
	// onlineapi
	OnlineAPIStartTime   string `json:"onlineapiStartTime,omitempty"`
	OnlineAPITarget      string `json:"onlineapiTarget,omitempty"`
	OnlineAPIKeyword     string `json:"onlineapiKeyword,omitempty"`
	OnlineAPISearchLimit int    `json:"onlineapiSearchLimit,omitempty"`
	// xonlineapi 任务需要区分是哪一个api
	IsFofa   bool `json:"fofa,omitempty"`
	IsHunter bool `json:"hunter,omitempty"`
	IsQuake  bool `json:"quake,omitempty"`
	// portscan
	IPPort       map[string][]int  `json:"ipport,omitempty"`       //IP:PORT列表
	IPPortString map[string]string `json:"ipportstring,omitempty"` //格式为ip列表，port可以为多种形式，如"80,443,8000-9000"、"--top-port 100"
	// domainscan : xdomainscan任务需要区分是哪一个子域名获取方式
	Domain             map[string]struct{} `json:"domain,omitempty"`
	IsSubDomainFinder  bool                `json:"subfinder,omitempty"`
	IsSubDomainBrute   bool                `json:"subdomainBrute,omitempty"`
	IsSubDomainCrawler bool                `json:"subdomainCrawler,omitempty"`
	// fingerprint
	IsFingerprint bool `json:"fingerprint,omitempty"`
	// xraypoc
	IsXrayPoc   bool   `json:"xraypoc,omitempty"`
	XrayPocFile string `json:"xraypocfile,omitempty"`
	// nucleipoc
	IsNucleiPoc   bool   `json:"nucleipoc,omitempty"`
	NucleiPocFile string `json:"nucleipocfile,omitempty"`
	// gobypoc
	IsGobyPoc bool `json:"gobypoc,omitempty"`
}

type XScan struct {
	Config       XScanConfig
	ResultIP     portscan.Result
	ResultDomain domainscan.Result
	ResultVul    []pocscan.Result
	vulMutex     sync.Mutex
}

var (
	portscanMaxThreadNum   = make(map[string]int)
	domainscanMaxThreadNum = make(map[string]int)
	xrayscanMaxThreadNum   = make(map[string]int)
)

func init() {
	portscanMaxThreadNum[conf.HighPerformance] = 4
	portscanMaxThreadNum[conf.NormalPerformance] = 2
	//
	domainscanMaxThreadNum[conf.HighPerformance] = 4
	domainscanMaxThreadNum[conf.NormalPerformance] = 2
	//
	xrayscanMaxThreadNum[conf.HighPerformance] = 4
	xrayscanMaxThreadNum[conf.NormalPerformance] = 2

}
func NewXScan(config XScanConfig) *XScan {
	x := XScan{Config: config}
	return &x
}

// XOrganization 根据组织ID获取资产，并进行IP和域名的任务
func XOrganization(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	if config.OrgId == nil || *config.OrgId == 0 {
		logging.RuntimeLog.Error("no org id")
		return FailedTask("no org id"), errors.New("no org id")
	}
	scan := NewXScan(config)
	// 根据ID读取资产
	if scan.Config.IsOrgIP {
		err = comm.CallXClient("LoadIpByOrgId", *config.OrgId, &scan.ResultIP.IPResult)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask("load org ip fail"), err
		}
		result = fmt.Sprintf("ip:%d", len(scan.ResultIP.IPResult))
	}
	if scan.Config.IsOrgDomain {
		err = comm.CallXClient("LoadDomainByOrgId", *config.OrgId, &scan.ResultDomain.DomainResult)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask("load org domain fail"), err

		}
		result = fmt.Sprintf("domain:%d", len(scan.ResultDomain.DomainResult))
	}
	// 执行portscan与domainscan
	ipPortMap, domainMap := MakeSubTaskTarget(&scan.ResultIP, &scan.ResultDomain)
	if len(ipPortMap) > 0 {
		// 指定了扫描的端口
		if len(config.OrgIPPort) > 0 {
			var ipPortMapString []map[string]string
			for _, ipm := range ipPortMap {
				ipp := make(map[string]string)
				for ip := range ipm {
					ipp[ip] = config.OrgIPPort
				}
				ipPortMapString = append(ipPortMapString, ipp)
			}
			_, err = scan.NewPortScan(taskId, mainTaskId, nil, ipPortMapString)
		} else {
			_, err = scan.NewPortScan(taskId, mainTaskId, ipPortMap, nil)
		}
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask(err.Error()), err
		}
	}
	// 域名任务只执行解析不进行子域名任务
	_, err = scan.NewDomainScan(taskId, mainTaskId, domainMap, false, false)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}

	return SucceedTask(result), nil
}

// XOnlineAPI Fofa任务
func XOnlineAPI(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.OnlineAPISearch(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行portscan与domainscan
	ipPortMap, domainMap := MakeSubTaskTarget(&scan.ResultIP, &scan.ResultDomain)
	_, err = scan.NewPortScan(taskId, mainTaskId, ipPortMap, nil)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	//域名任务只执行解析不进行子域名任务
	_, err = scan.NewDomainScan(taskId, mainTaskId, domainMap, false, false)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}

	return SucceedTask(result), nil
}

// XPortScan 端口扫描任务
func XPortScan(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.Portscan(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 启动指纹识别任务：
	if config.IsFingerprint {
		_, err = scan.NewFingerprintScan(taskId, mainTaskId)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask(err.Error()), err
		}
	}
	//
	return SucceedTask(result), nil
}

// XDomainscan 域名任务
func XDomainscan(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.Domainscan(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 启动指纹识别任务：
	if config.IsFingerprint {
		_, err = scan.NewFingerprintScan(taskId, mainTaskId)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask(err.Error()), err
		}
	}
	return SucceedTask(result), nil
}

// XFingerPrint 指纹识别任务
func XFingerPrint(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.FingerPrint(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 启动XrayPoc任务
	if config.IsXrayPoc {
		_, err = scan.NewXrayScan(taskId, mainTaskId)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask(err.Error()), err
		}
	}

	// 启动NucleiPoc任务
	if config.IsNucleiPoc {
		_, err = scan.NewNucleiScan(taskId, mainTaskId)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask(err.Error()), err
		}
	}

	// 启动GobyPoc任务
	if config.IsGobyPoc {
		_, err = scan.NewGobyScan(taskId, mainTaskId)
		if err != nil {
			logging.RuntimeLog.Error(err)
			return FailedTask(err.Error()), err
		}
	}
	return SucceedTask(result), nil
}

// XXray Xray扫描任务（调用xray二进制程序）
func XXray(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.XrayScan(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	return SucceedTask(result), nil
}

// XNuclei Nuclei扫描任务（调用Nuclei二进制程序）
func XNuclei(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.NucleiScan(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	return SucceedTask(result), nil
}

// XGoby goby扫描任务（调用goby二进制程序）
func XGoby(taskId, mainTaskId, configJSON string) (result string, err error) {
	// 检查任务状态
	var ok bool
	if ok, result, err = CheckTaskStatus(taskId); !ok {
		return result, err
	}
	// 解析任务参数
	config := XScanConfig{}
	if err = ParseConfig(configJSON, &config); err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	// 执行任务
	scan := NewXScan(config)
	result, err = scan.GobyScan(taskId, mainTaskId)
	if err != nil {
		logging.RuntimeLog.Error(err)
		return FailedTask(err.Error()), err
	}
	return SucceedTask(result), nil
}

// Portscan 执行端口扫描，通过协程并发执行
func (x *XScan) Portscan(taskId string, mainTaskId string) (result string, err error) {
	x.ResultIP.IPResult = make(map[string]*portscan.IPResult)

	swg := sizedwaitgroup.New(portscanMaxThreadNum[conf.WorkerPerformanceMode])
	// 生成扫描参数
	conf.GlobalWorkerConfig().ReloadConfig()
	config := portscan.Config{
		OrgId:        x.Config.OrgId,
		Rate:         conf.GlobalWorkerConfig().Portscan.Rate,
		IsPing:       conf.GlobalWorkerConfig().Portscan.IsPing,
		Tech:         conf.GlobalWorkerConfig().Portscan.Tech,
		CmdBin:       conf.GlobalWorkerConfig().Portscan.Cmdbin,
		IsIpLocation: true,
		WorkspaceId:  x.Config.WorkspaceId,
	}
	if len(x.Config.IPPortString) > 0 {
		for ip, ports := range x.Config.IPPortString {
			if len(ports) <= 0 {
				continue
			}
			runConfig := config
			runConfig.Target = ip
			runConfig.Port = ports
			swg.Add()
			//执行扫描
			go x.doPortscan(&swg, runConfig)
		}
	}
	if len(x.Config.IPPort) > 0 {
		for ip, ports := range x.Config.IPPort {
			if len(ports) <= 0 {
				continue
			}
			//按IP执行扫描任务
			var ps []string
			for _, p := range ports {
				ps = append(ps, fmt.Sprintf("%d", p))
			}
			runConfig := config
			runConfig.Target = ip
			runConfig.Port = strings.Join(ps, ",")
			swg.Add()
			//执行扫描
			go x.doPortscan(&swg, runConfig)
		}
	}
	swg.Wait()
	// 保存结果
	resultArgs := comm.ScanResultArgs{
		TaskID:     taskId,
		MainTaskId: mainTaskId,
		IPConfig:   &portscan.Config{OrgId: config.OrgId, WorkspaceId: config.WorkspaceId},
		IPResult:   x.ResultIP.IPResult,
	}
	err = comm.CallXClient("SaveScanResult", &resultArgs, &result)
	if err != nil {
		logging.RuntimeLog.Error(err)
	}
	return
}

// doPortscan 调用一次端口扫描
func (x *XScan) doPortscan(swg *sizedwaitgroup.SizedWaitGroup, config portscan.Config) {
	defer swg.Done()

	var result portscan.Result
	if config.CmdBin == "masnmap" {
		result.IPResult = doMasscanPlusNmap(config).IPResult
	} else if config.CmdBin == "masscan" {
		m := portscan.NewMasscan(config)
		m.Do()
		result.IPResult = m.Result.IPResult
	} else {
		m := portscan.NewNmap(config)
		m.Do()
		result.IPResult = m.Result.IPResult
	}

	//增加ip归属地查询,先判断是否合规，再进行查询归属地
	if utils.CheckIPV4Subnet(config.Target) == false {
		doLocation(&result)
	}

	//合并结果
	x.ResultIP.Lock()
	for k, v := range result.IPResult {
		x.ResultIP.IPResult[k] = v
	}
	x.ResultIP.Unlock()
}

// doDomainscan 调用执行一次域名任务
func (x *XScan) doDomainscan(swg *sizedwaitgroup.SizedWaitGroup, config domainscan.Config) {
	defer swg.Done()

	var result domainscan.Result
	//扫描
	result = doDomainScan(config)
	//合并结果
	x.ResultDomain.Lock()
	for k, v := range result.DomainResult {
		x.ResultDomain.DomainResult[k] = v
	}
	x.ResultDomain.Unlock()
}

// doXrayscan 调用一次Xray
func (x *XScan) doXrayscan(swg *sizedwaitgroup.SizedWaitGroup, config pocscan.Config) {
	defer swg.Done()

	xray := pocscan.NewXray(config)
	xray.Do()
	//合并结果
	x.vulMutex.Lock()
	x.ResultVul = append(x.ResultVul, xray.Result...)
	x.vulMutex.Unlock()
}

// doNucleiScan 调用一次Nuclei
func (x *XScan) doNucleiScan(swg *sizedwaitgroup.SizedWaitGroup, config pocscan.Config) {
	defer swg.Done()

	nuclei := pocscan.NewNuclei(config)
	nuclei.Do()
	//合并结果
	x.vulMutex.Lock()
	x.ResultVul = append(x.ResultVul, nuclei.Result...)
	x.vulMutex.Unlock()
}

// doGobyScan 调用一次Goby
func (x *XScan) doGobyScan(swg *sizedwaitgroup.SizedWaitGroup, config pocscan.Config) {
	defer swg.Done()

	goby := pocscan.NewGoby(config)
	goby.Do()
	//合并结果
	x.vulMutex.Lock()
	x.ResultVul = append(x.ResultVul, goby.Result...)
	x.vulMutex.Unlock()
}

// OnlineAPISearch 执行fofa搜索任务
func (x *XScan) OnlineAPISearch(taskId string, mainTaskId string) (result string, err error) {
	conf.GlobalWorkerConfig().ReloadConfig()
	config := onlineapi.OnlineAPIConfig{
		OrgId:           x.Config.OrgId,
		IsIPLocation:    true,
		SearchStartTime: x.Config.OnlineAPIStartTime,
		// 从配置文件默认参数获取：
		IsIgnoreCDN:        conf.GlobalWorkerConfig().Domainscan.IsIgnoreCDN,
		IsIgnoreOutofChina: conf.GlobalWorkerConfig().Domainscan.IsIgnoreOutofChina,
		WorkspaceId:        x.Config.WorkspaceId,
	}
	if x.Config.IsFingerprint {
		config.IsHttpx = conf.GlobalWorkerConfig().Fingerprint.IsHttpx
		config.IsScreenshot = conf.GlobalWorkerConfig().Fingerprint.IsScreenshot
		config.IsFingerprintHub = conf.GlobalWorkerConfig().Fingerprint.IsFingerprintHub
		config.IsIconHash = conf.GlobalWorkerConfig().Fingerprint.IsIconHash
	}
	//fofa任务支持两种模式：
	//一种是关键词，需设置SearchByKeyWord为true，只支持fofa
	//另一种是ip/domain，同时支持fofa、quake、hunter
	if len(x.Config.OnlineAPIKeyword) > 0 {
		config.SearchByKeyWord = true
		config.Target = x.Config.OnlineAPIKeyword
		config.SearchLimitCount = x.Config.OnlineAPISearchLimit
		//x.ResultIP, x.ResultDomain, result, err = doOnlineAPIAndSave(taskId, mainTaskId, x.Config.On, config)
	} else if len(x.Config.OnlineAPITarget) > 0 {
		config.Target = x.Config.OnlineAPITarget
	}
	if x.Config.IsFofa {
		x.ResultIP, x.ResultDomain, result, err = doOnlineAPIAndSave(taskId, mainTaskId, "fofa", config)
	}
	if x.Config.IsQuake {
		x.ResultIP, x.ResultDomain, result, err = doOnlineAPIAndSave(taskId, mainTaskId, "quake", config)
	}
	if x.Config.IsHunter {
		x.ResultIP, x.ResultDomain, result, err = doOnlineAPIAndSave(taskId, mainTaskId, "hunter", config)
	}
	return
}

// Domainscan 执行域名任务
func (x *XScan) Domainscan(taskId string, mainTaskId string) (result string, err error) {
	x.ResultDomain.DomainResult = make(map[string]*domainscan.DomainResult)
	swg := sizedwaitgroup.New(domainscanMaxThreadNum[conf.WorkerPerformanceMode])

	conf.GlobalWorkerConfig().ReloadConfig()
	config := domainscan.Config{
		OrgId: x.Config.OrgId,
		// domain方法：
		IsSubDomainFinder: x.Config.IsSubDomainFinder,
		IsSubDomainBrute:  x.Config.IsSubDomainBrute,
		IsCrawler:         x.Config.IsSubDomainCrawler,
		//
		IsIgnoreCDN:        conf.GlobalWorkerConfig().Domainscan.IsIgnoreCDN,
		IsIgnoreOutofChina: conf.GlobalWorkerConfig().Domainscan.IsIgnoreOutofChina,
		IsIPPortScan:       conf.GlobalWorkerConfig().Domainscan.IsPortScan,

		WorkspaceId: x.Config.WorkspaceId,
	}
	for domain := range x.Config.Domain {
		runConfig := config
		runConfig.Target = domain
		swg.Add()
		go x.doDomainscan(&swg, runConfig)
	}
	swg.Wait()
	// 如果有端口扫描的选项
	if config.IsIPPortScan || config.IsIPSubnetPortScan {
		doPortScanByDomainscan(taskId, mainTaskId, config, &x.ResultDomain)
	}
	// 保存结果
	resultArgs := comm.ScanResultArgs{
		TaskID:       taskId,
		MainTaskId:   mainTaskId,
		DomainConfig: &domainscan.Config{OrgId: config.OrgId, WorkspaceId: x.Config.WorkspaceId},
		DomainResult: x.ResultDomain.DomainResult,
	}
	if err = comm.CallXClient("SaveScanResult", &resultArgs, &result); err != nil {
		logging.RuntimeLog.Error(err)
	}
	return
}

// NewPortScan 根据IP/port列表，生成端口扫描任务
func (x *XScan) NewPortScan(taskId, mainTaskId string, ipPortMap []map[string][]int, ipPortMapString []map[string]string) (result string, err error) {
	config := XScanConfig{
		OrgId:         x.Config.OrgId,
		IsFingerprint: x.Config.IsFingerprint,
		IsXrayPoc:     x.Config.IsXrayPoc,
		XrayPocFile:   x.Config.XrayPocFile,
		IsNucleiPoc:   x.Config.IsNucleiPoc,
		NucleiPocFile: x.Config.NucleiPocFile,
		IsGobyPoc:     x.Config.IsGobyPoc,
		WorkspaceId:   x.Config.WorkspaceId,
	}
	for _, t := range ipPortMap {
		configRun := config
		configRun.IPPort = t
		result, err = sendTask(taskId, mainTaskId, configRun, "xportscan")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	for _, t := range ipPortMapString {
		configRun := config
		configRun.IPPortString = t
		result, err = sendTask(taskId, mainTaskId, configRun, "xportscan")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	return
}

// NewICPQuery 生成ICP查询任务
func (x *XScan) NewICPQuery(taskId, mainTaskId string, target string) (result string, err error) {
	config := onlineapi.ICPQueryConfig{Target: target}
	if err != nil {
		logging.RuntimeLog.Errorf("start icpquery fail:%s", err.Error())
		return "", err
	}
	result, err = sendTask(taskId, mainTaskId, config, "icpquery")
	if err != nil {
		logging.RuntimeLog.Errorf("start icpquery fail:%s", err.Error())
		return "", err
	}
	return result, nil
}

// NewWhoisQuery 生成whois查询任务
func (x *XScan) NewWhoisQuery(taskId, mainTaskId string, target string) (result string, err error) {
	config := onlineapi.WhoisQueryConfig{Target: target}
	if err != nil {
		logging.RuntimeLog.Errorf("start whoisquery fail:%s", err.Error())
		return "", err
	}
	result, err = sendTask(taskId, mainTaskId, config, "whoisquery")
	if err != nil {
		logging.RuntimeLog.Errorf("start whoisquery fail:%s", err.Error())
		return "", err
	}
	return result, nil
}

// NewDomainScan 根据域名列表，生成域名任务
func (x *XScan) NewDomainScan(taskId, mainTaskId string, domainMap []map[string]struct{}, isSubDomainFinder, isSubDomainBrute bool) (result string, err error) {
	config := XScanConfig{
		OrgId:             x.Config.OrgId,
		IsSubDomainFinder: isSubDomainFinder,
		IsSubDomainBrute:  isSubDomainBrute,
		IsFingerprint:     x.Config.IsFingerprint,
		IsXrayPoc:         x.Config.IsXrayPoc,
		XrayPocFile:       x.Config.XrayPocFile,
		IsNucleiPoc:       x.Config.IsNucleiPoc,
		NucleiPocFile:     x.Config.NucleiPocFile,
		IsGobyPoc:         x.Config.IsGobyPoc,
		WorkspaceId:       x.Config.WorkspaceId,
	}
	for _, t := range domainMap {
		configRun := config
		configRun.Domain = t
		result, err = sendTask(taskId, mainTaskId, configRun, "xdomainscan")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	return
}

// FingerPrint 执行指纹识别任务
func (x *XScan) FingerPrint(taskId string, mainTaskId string) (result string, err error) {
	conf.GlobalWorkerConfig().ReloadConfig()
	config := FingerprintTaskConfig{
		// 从配置文件默认参数获取：
		IsHttpx:          conf.GlobalWorkerConfig().Fingerprint.IsHttpx,
		IsScreenshot:     conf.GlobalWorkerConfig().Fingerprint.IsScreenshot,
		IsFingerprintHub: conf.GlobalWorkerConfig().Fingerprint.IsFingerprintHub,
		IsIconHash:       conf.GlobalWorkerConfig().Fingerprint.IsIconHash,
		IPTargetMap:      x.Config.IPPort,
		DomainTargetMap:  x.Config.Domain,
		WorkspaceId:      x.Config.WorkspaceId,
	}
	x.ResultIP, x.ResultDomain, result, err = doFingerPrintAndSave(taskId, mainTaskId, config)

	return
}

// NewFingerprintScan 生成指纹识别任务
func (x *XScan) NewFingerprintScan(taskId, mainTaskId string) (result string, err error) {
	// 由于fingerprint会影响后续的pocscan，所以这里必须生成fingerprint任务
	//if x.Config.IsHttpx == false && x.Config.IsFingerprintHub == false && x.Config.IsIconHash == false && x.Config.IsScreenshot == false {
	//	return
	//}
	config := XScanConfig{
		IsXrayPoc:     x.Config.IsXrayPoc,
		XrayPocFile:   x.Config.XrayPocFile,
		IsNucleiPoc:   x.Config.IsNucleiPoc,
		NucleiPocFile: x.Config.NucleiPocFile,
		IsGobyPoc:     x.Config.IsGobyPoc,
		WorkspaceId:   x.Config.WorkspaceId,
	}
	//拆分子任务
	ipTarget, domainTarget := MakeSubTaskTarget(&x.ResultIP, &x.ResultDomain)
	//生成任务
	for _, t := range ipTarget {
		newConfig := config
		newConfig.IPPort = t
		result, err = sendTask(taskId, mainTaskId, newConfig, "xfingerprint")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	for _, t := range domainTarget {
		newConfig := config
		newConfig.Domain = t
		result, err = sendTask(taskId, mainTaskId, newConfig, "xfingerprint")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	return
}

// NewNucleiScan 生成Nuclei任务
func (x *XScan) NewNucleiScan(taskId, mainTaskId string) (result string, err error) {
	//拆分子任务
	ipTarget, domainTarget := MakeSubTaskTarget(&x.ResultIP, &x.ResultDomain)
	for _, t := range ipTarget {
		newConfig := XScanConfig{IPPort: t, IsNucleiPoc: true, NucleiPocFile: x.Config.NucleiPocFile, WorkspaceId: x.Config.WorkspaceId}
		result, err = sendTask(taskId, mainTaskId, newConfig, "xnuclei")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	for _, t := range domainTarget {
		newConfig := XScanConfig{Domain: t, IsNucleiPoc: true, NucleiPocFile: x.Config.NucleiPocFile, WorkspaceId: x.Config.WorkspaceId}
		result, err = sendTask(taskId, mainTaskId, newConfig, "xnuclei")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	return
}

// NewGobyScan 生成Goby任务
func (x *XScan) NewGobyScan(taskId, mainTaskId string) (result string, err error) {
	//拆分子任务
	ipTarget, domainTarget := MakeSubTaskTarget(&x.ResultIP, &x.ResultDomain)
	for _, t := range ipTarget {
		newConfig := XScanConfig{IPPort: t, IsGobyPoc: true, WorkspaceId: x.Config.WorkspaceId}
		result, err = sendTask(taskId, mainTaskId, newConfig, "xgoby")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	for _, t := range domainTarget {
		newConfig := XScanConfig{Domain: t, IsGobyPoc: true, WorkspaceId: x.Config.WorkspaceId}
		result, err = sendTask(taskId, mainTaskId, newConfig, "xgoby")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	return
}

// NucleiScan 调用执行Nuclei扫描任务
func (x *XScan) NucleiScan(taskId string, mainTaskId string) (result string, err error) {
	// 生成扫描参数
	config := pocscan.Config{PocFile: x.Config.NucleiPocFile, WorkspaceId: x.Config.WorkspaceId}
	if x.Config.NucleiPocFile == "" {
		config.PocFile = "*"
	}
	swg := sizedwaitgroup.New(xrayscanMaxThreadNum[conf.WorkerPerformanceMode])
	if len(x.Config.IPPort) > 0 {
		for ip, ports := range x.Config.IPPort {
			for _, port := range ports {
				runConfig := config
				runConfig.Target = fmt.Sprintf("%s:%d", ip, port)
				swg.Add()
				go x.doNucleiScan(&swg, runConfig)
			}
		}
	}
	if len(x.Config.Domain) > 0 {
		for domain := range x.Config.Domain {
			runConfig := config
			runConfig.Target = domain
			swg.Add()
			go x.doNucleiScan(&swg, runConfig)
		}
	}
	swg.Wait()
	// 保存结果
	resultArgs := comm.ScanResultArgs{
		TaskID:              taskId,
		MainTaskId:          mainTaskId,
		VulnerabilityResult: x.ResultVul,
	}
	err = comm.CallXClient("SaveVulnerabilityResult", &resultArgs, &result)
	if err != nil {
		logging.RuntimeLog.Error(err)
	}
	return
}

// GobyScan 调用执行goby扫描任务
func (x *XScan) GobyScan(taskId string, mainTaskId string) (result string, err error) {
	// 生成扫描参数
	config := pocscan.Config{WorkspaceId: x.Config.WorkspaceId}
	// goby支持通过,分隔的多个目标
	swg := sizedwaitgroup.New(xrayscanMaxThreadNum[conf.WorkerPerformanceMode])
	if len(x.Config.IPPort) > 0 {
		var targets []string
		for ip, ports := range x.Config.IPPort {
			for _, port := range ports {
				targets = append(targets, fmt.Sprintf("%s:%d", ip, port))
			}
		}
		runConfig := config
		runConfig.Target = strings.Join(targets, ",")
		swg.Add()
		go x.doGobyScan(&swg, runConfig)
	}
	if len(x.Config.Domain) > 0 {
		var targets []string
		for domain := range x.Config.Domain {
			targets = append(targets, domain)
		}
		runConfig := config
		runConfig.Target = strings.Join(targets, ",")
		swg.Add()
		go x.doGobyScan(&swg, runConfig)
	}
	swg.Wait()
	// 保存结果
	resultArgs := comm.ScanResultArgs{
		TaskID:              taskId,
		MainTaskId:          mainTaskId,
		VulnerabilityResult: x.ResultVul,
	}
	err = comm.CallXClient("SaveVulnerabilityResult", &resultArgs, &result)
	if err != nil {
		logging.RuntimeLog.Error(err)
	}
	return
}

// NewXrayScan 生成xraypoc任务
func (x *XScan) NewXrayScan(taskId, mainTaskId string) (result string, err error) {
	//拆分子任务
	ipTarget, domainTarget := MakeSubTaskTarget(&x.ResultIP, &x.ResultDomain)
	for _, t := range ipTarget {
		newConfig := XScanConfig{IPPort: t, IsXrayPoc: true, XrayPocFile: x.Config.XrayPocFile, WorkspaceId: x.Config.WorkspaceId}
		result, err = sendTask(taskId, mainTaskId, newConfig, "xxray")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	for _, t := range domainTarget {
		newConfig := XScanConfig{Domain: t, IsXrayPoc: true, XrayPocFile: x.Config.XrayPocFile, WorkspaceId: x.Config.WorkspaceId}
		result, err = sendTask(taskId, mainTaskId, newConfig, "xxray")
		if err != nil {
			logging.RuntimeLog.Error(err)
			return
		}
	}
	return
}

// XrayScan 调用执行xray扫描任务
func (x *XScan) XrayScan(taskId string, mainTaskId string) (result string, err error) {
	// 生成扫描参数
	config := pocscan.Config{PocFile: x.Config.XrayPocFile, WorkspaceId: x.Config.WorkspaceId}
	if x.Config.XrayPocFile == "" {
		config.PocFile = "*"
	}
	swg := sizedwaitgroup.New(xrayscanMaxThreadNum[conf.WorkerPerformanceMode])
	if len(x.Config.IPPort) > 0 {
		for ip, ports := range x.Config.IPPort {
			for _, port := range ports {
				runConfig := config
				runConfig.Target = fmt.Sprintf("%s:%d", ip, port)
				swg.Add()
				go x.doXrayscan(&swg, runConfig)
			}
		}
	}
	if len(x.Config.Domain) > 0 {
		for domain := range x.Config.Domain {
			runConfig := config
			runConfig.Target = domain
			swg.Add()
			go x.doXrayscan(&swg, runConfig)
		}
	}
	swg.Wait()
	// 保存结果
	resultArgs := comm.ScanResultArgs{
		TaskID:              taskId,
		MainTaskId:          mainTaskId,
		VulnerabilityResult: x.ResultVul,
	}
	err = comm.CallXClient("SaveVulnerabilityResult", &resultArgs, &result)
	if err != nil {
		logging.RuntimeLog.Error(err)
	}
	return
}
