package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	currentContentModerationBuiltinRegexDefaultsVersion = 2
	legacyContentModerationBuiltinRegexRuleCount        = 66

	defaultContentModerationBuiltinRegexThreshold       = 50
	defaultContentModerationBuiltinRegexStrictThreshold = 90
	maxContentModerationBuiltinRegexThreshold           = 500
	maxContentModerationBuiltinRegexStrictThreshold     = 1000
	maxContentModerationBuiltinRegexRules               = 256
	maxContentModerationBuiltinRegexRuleNameRunes       = 100
	maxContentModerationBuiltinRegexCategoryRunes       = 100
	maxContentModerationBuiltinRegexPatternRunes        = 4096
	maxContentModerationBuiltinRegexRuleWeight          = 1000
)

// The built-in rules are adapted from james-6-23/codex2api's prompt filter:
// https://github.com/james-6-23/codex2api/blob/8a6f7f5eca4437dc627c162dc4e8fda4531d2da2/security/promptfilter/patterns.go
// The upstream project declares the code as MIT licensed in its README. See
// THIRD_PARTY_NOTICES.md for attribution and license text.
type ContentModerationRegexRule struct {
	Name     string `json:"name"`
	Pattern  string `json:"pattern"`
	Weight   int    `json:"weight"`
	Category string `json:"category"`
	Strict   bool   `json:"strict"`
}

var contentModerationBuiltinRegexRuleDefinitions = []ContentModerationRegexRule{
	{Name: "credential_theft", Pattern: `(?i)(?:^|[.!?。！？]\s*)(steal|dump|extract|exfiltrate|harvest|grab)\b.{0,50}\b(?:credentials?|passwords?|tokens?|cookies?)\b|\b(write|generate|create|give|build|craft|make|show|provide|implement|code|script|tool|steps?|instructions?|how\s+to|how\s+(?:can|do)\s+i|help\s+me|i\s+want\s+to|please|can\s+you)\b.{0,100}\b(steal|dump|extract|exfiltrate|harvest|grab)\b.{0,50}\b(?:credentials?|passwords?|tokens?|cookies?)\b|(?:写|生成|给我|构造|制作|提供|实现).{0,50}(窃取|导出|转储|提取).{0,30}(凭证|密码|令牌|token|cookie)`, Weight: 100, Category: "malicious", Strict: true},
	{Name: "malware_family", Pattern: `(?i)\b(keylogger|ransomware|trojan|backdoor|botnet|infostealer)\b`, Weight: 80, Category: "malware", Strict: true},
	{Name: "evasion", Pattern: `(?i)\b(bypass|disable|evade)\s+(av|edr|defender|antivirus|endpoint\s+detection)\b|免杀|绕过\s*(杀软|edr)`, Weight: 80, Category: "evasion", Strict: true},
	{Name: "persistence", Pattern: `(?i)\b(persistence|persist(?:ent)?\s+access|startup\s+persistence|registry\s+run\s+key)\b`, Weight: 35, Category: "post_exploitation"},
	{Name: "remote_shell", Pattern: `(?i)\b(reverse\s+shell|bind\s+shell|meterpreter|web\s+shell)\b|反弹\s*shell`, Weight: 55, Category: "remote_access"},
	{Name: "operational_remote_access_request", Pattern: `(?i)\b(write|generate|create|give|build|craft|make)\b.{0,80}\b(reverse\s+shell|bind\s+shell|meterpreter|web\s+shell)\b|(?:写|生成|给我|构造|制作).{0,40}反弹\s*shell`, Weight: 20, Category: "remote_access"},
	{Name: "exploit_payload", Pattern: `(?i)\b(exploit\s+payload|payload\s+for\s+exploiting|weaponiz(?:e|ed|ation))\b`, Weight: 45, Category: "exploit"},
	{Name: "operational_exploit_request", Pattern: `(?i)\b(write|generate|create|give|build|craft|make)\b.{0,80}\b(exploit(?:\s+payload)?|payload\s+(?:for|to)\s+exploit|poc|proof[-\s]?of[-\s]?concept|0day|zero[-\s]?day)\b|(?:写|生成|给我|构造|制作).{0,40}(漏洞利用|攻击载荷|payload|poc)`, Weight: 45, Category: "exploit"},
	{Name: "exploit_technique", Pattern: `(?i)\b(shellcode|rop\s+chain|heap\s+spray|buffer\s+overflow\s+exploit)\b`, Weight: 35, Category: "exploit"},
	{Name: "privilege_escalation", Pattern: `(?i)\b(privilege\s+escalation|privesc|root\s+exploit|local\s+root)\b|提权`, Weight: 35, Category: "post_exploitation"},
	{Name: "pentest_tooling", Pattern: `(?i)\b(metasploit|cobalt\s+strike|mimikatz|empire|sliver\s+c2)\b`, Weight: 30, Category: "tooling"},
	{Name: "scanner_tooling", Pattern: `(?i)\b(sqlmap|nmap|masscan|zmap|burp\s+suite)\b`, Weight: 15, Category: "tooling"},
	{Name: "large_scale_scanning", Pattern: `(?i)\b(large[-\s]?scale|internet[-\s]?wide|public\s+ip\s+ranges?|mass)\s+(scan|scanning|enumeration)\b`, Weight: 40, Category: "scanning"},
	{Name: "cve_reference", Pattern: `(?i)\bcve-\d{4}-\d{4,7}\b`, Weight: 10, Category: "vulnerability"},
	{Name: "generic_exploit", Pattern: `(?i)\b(exploit|vulnerability|0day|zero[-\s]?day)\b`, Weight: 10, Category: "vulnerability"},
	{Name: "reverse_engineering", Pattern: `(?i)\b(ida\s+pro|ghidra|x64dbg|ollydbg|frida\s+hook|deobfuscate|unpack)\b|反编译|脱壳`, Weight: 15, Category: "reverse_engineering"},
	{Name: "reverse_engineering_secret_extraction", Pattern: `(?i)\b(ida\s+pro|ghidra|x64dbg|ollydbg|frida|jadx|apktool|decompile|disassembl|reverse\s+engineer)\b.{0,120}\b(extract|dump|recover|decrypt)\b.{0,80}\b(api\s*keys?|tokens?|secrets?|private\s*keys?|certificates?|license\s*keys?)\b|(?:ida|ghidra|frida|jadx|apktool|反编译|逆向).{0,80}(提取|导出|解密|恢复).{0,40}(密钥|token|令牌|私钥|证书|授权码)`, Weight: 90, Category: "reverse_engineering", Strict: true},
	{Name: "reverse_engineering_license_bypass", Pattern: `(?i)\b(ida\s+pro|ghidra|x64dbg|ollydbg|frida|jadx|apktool|decompile|disassembl|reverse\s+engineer)\b.{0,120}\b(bypass|crack|patch|remove|unlock)\b.{0,80}\b(license|activation|trial|paywall|subscription|in[-\s]?app\s+purchase|iap|entitlement)\b|(?:ida|ghidra|x64dbg|frida|反编译|逆向|脱壳|调试).{0,80}(绕过|破解|补丁|去除|解锁).{0,40}(授权|激活|试用|会员|订阅|付费|内购)`, Weight: 85, Category: "license_cracking", Strict: true},
	{Name: "reverse_engineering_anti_debug_bypass", Pattern: `(?i)\b(bypass|disable|remove|defeat)\b.{0,60}\b(anti[-\s]?debug|anti[-\s]?tamper|integrity\s+check|root\s+detection|jailbreak\s+detection|certificate\s+pinning)\b|绕过.{0,40}(反调试|反篡改|完整性校验|root\s*检测|越狱检测|证书绑定|证书固定)`, Weight: 70, Category: "reverse_engineering", Strict: true},
	{Name: "frida_hook_abuse", Pattern: `(?i)\b(frida|substrate|xposed)\b.{0,100}\b(hook|patch|bypass|unlock)\b.{0,80}\b(payment|purchase|license|activation|subscription|login|auth|entitlement)\b|(?:frida|xposed).{0,80}(hook|绕过|破解|解锁).{0,40}(支付|内购|授权|激活|会员|订阅|登录|鉴权)`, Weight: 75, Category: "reverse_engineering", Strict: true},
	{Name: "license_cracking", Pattern: `(?i)\b(keygen|crack\s+license|serial\s+generator|license\s+bypass|patch\s+(activation|license))\b|注册机|破解授权|序列号生成`, Weight: 55, Category: "license_cracking", Strict: true},
	{Name: "data_exfiltration", Pattern: `(?i)\b(exfiltrate|exfiltration|data\s+theft|steal\s+data|siphon\s+data)\b.{0,80}\b(database|files?|documents?|source\s+code|intellectual\s+property)\b|数据窃取|数据外泄`, Weight: 70, Category: "data_theft", Strict: true},
	{Name: "ddos_attack", Pattern: `(?i)\b(ddos|dos\s+attack|distributed\s+denial|amplification\s+attack|syn\s+flood|udp\s+flood)\b|拒绝服务攻击|流量攻击`, Weight: 65, Category: "network_attack", Strict: true},
	{Name: "cryptomining_hijack", Pattern: `(?i)\b(cryptojacking|coinhive|monero\s+miner|unauthorized\s+mining|hijack.{0,40}mining)\b|挖矿劫持|非法挖矿`, Weight: 60, Category: "resource_abuse", Strict: true},
	{Name: "phishing_social_engineering", Pattern: `(?i)\b(phishing\s+(page|site|email)|credential\s+harvesting|fake\s+login|spoof\s+(domain|website))\b|钓鱼页面|伪造登录`, Weight: 75, Category: "social_engineering", Strict: true},
	{Name: "supply_chain_attack", Pattern: `(?i)\b(supply\s+chain\s+attack|dependency\s+confusion|typosquatting|malicious\s+package|backdoor.{0,40}(npm|pypi|gem))\b|供应链攻击|依赖投毒`, Weight: 70, Category: "supply_chain", Strict: true},
	{Name: "container_escape", Pattern: `(?i)\b(container\s+escape|docker\s+breakout|kubernetes\s+escape|privileged\s+container\s+exploit)\b|容器逃逸`, Weight: 50, Category: "container_security"},
	{Name: "cloud_abuse", Pattern: `(?i)\b(aws\s+key\s+leak|gcp\s+credential|azure\s+token|s3\s+bucket\s+takeover|iam\s+privilege\s+escalation)\b|云凭证泄露`, Weight: 55, Category: "cloud_security"},
	{Name: "sql_injection_attack", Pattern: `(?i)\b(sql\s+injection\s+payload|union\s+select\s+attack|blind\s+sqli|time[-\s]?based\s+sqli)\b|sql注入攻击`, Weight: 40, Category: "web_attack"},
	{Name: "command_injection", Pattern: `(?i)\b(command\s+injection|os\s+command\s+injection|shell\s+injection|rce\s+exploit)\b|命令注入`, Weight: 50, Category: "web_attack"},
	{Name: "ssrf_xxe_attack", Pattern: `(?i)\b(ssrf\s+exploit|server[-\s]?side\s+request\s+forgery|xxe\s+attack|xml\s+external\s+entity)\b`, Weight: 35, Category: "web_attack"},
	{Name: "password_cracking", Pattern: `(?i)\b(hashcat|john\s+the\s+ripper|password\s+cracking|brute[-\s]?force\s+(password|hash)|rainbow\s+table)\b|密码破解|暴力破解`, Weight: 30, Category: "credential_attack"},
	{Name: "mitm_attack", Pattern: `(?i)\b(man[-\s]?in[-\s]?the[-\s]?middle|mitm\s+attack|arp\s+spoofing|dns\s+spoofing|ssl\s+strip)\b|中间人攻击`, Weight: 45, Category: "network_attack"},
	{Name: "wireless_attack", Pattern: `(?i)\b(wpa2?\s+crack|wifi\s+deauth|evil\s+twin|rogue\s+access\s+point|aircrack)\b|wifi破解|无线攻击`, Weight: 35, Category: "wireless_attack"},
	{Name: "firmware_iot_exploit", Pattern: `(?i)\b(firmware\s+extraction|iot\s+exploit|router\s+backdoor|embedded\s+device\s+hack)\b|固件提取|物联网攻击`, Weight: 40, Category: "iot_security"},
	{Name: "blockchain_exploit", Pattern: `(?i)\b(smart\s+contract\s+exploit|reentrancy\s+attack|flash\s+loan\s+attack|private\s+key\s+theft)\b|智能合约漏洞|私钥窃取`, Weight: 45, Category: "blockchain_security"},
	{Name: "session_hijacking", Pattern: `(?i)\b(session\s+hijacking|cookie\s+theft|session\s+fixation|csrf\s+exploit)\b|会话劫持|cookie窃取`, Weight: 40, Category: "web_attack"},
	{Name: "api_abuse", Pattern: `(?i)\b(api\s+key\s+leak|rate\s+limit\s+bypass|api\s+abuse|unauthorized\s+api\s+access)\b|api密钥泄露|接口滥用`, Weight: 35, Category: "api_security"},
	{Name: "steganography_covert", Pattern: `(?i)\b(steganography|covert\s+channel|data\s+hiding|exfiltration\s+via\s+(dns|icmp))\b|隐写术|隐蔽信道`, Weight: 30, Category: "evasion"},
	{Name: "ransomware_deployment", Pattern: `(?i)\b(deploy\s+ransomware|ransomware\s+payload|encrypt\s+files\s+for\s+ransom|wannacry|locky)\b|部署勒索软件|加密勒索`, Weight: 90, Category: "malware", Strict: true},
	{Name: "botnet_c2", Pattern: `(?i)\b(botnet\s+command|c2\s+server|command\s+and\s+control|zombie\s+network)\b|僵尸网络|c2服务器`, Weight: 65, Category: "malware", Strict: true},
	{Name: "xss_attack", Pattern: `(?i)\b(xss\s+payload|cross[-\s]?site\s+scripting\s+attack|stored\s+xss|reflected\s+xss|dom\s+xss)\b|xss攻击载荷`, Weight: 35, Category: "web_attack"},
	{Name: "deserialization_exploit", Pattern: `(?i)\b(deserialization\s+exploit|insecure\s+deserialization|java\s+deserialization\s+attack|pickle\s+exploit)\b|反序列化漏洞`, Weight: 45, Category: "web_attack"},
	{Name: "path_traversal", Pattern: `(?i)\b(path\s+traversal|directory\s+traversal|\.\.\/|lfi\s+exploit|local\s+file\s+inclusion)\b|目录遍历|文件包含`, Weight: 35, Category: "web_attack"},
	{Name: "memory_corruption", Pattern: `(?i)\b(use[-\s]?after[-\s]?free|double\s+free|heap\s+overflow|stack\s+overflow\s+exploit)\b|内存破坏|堆溢出`, Weight: 50, Category: "exploit"},
	{Name: "kernel_exploit", Pattern: `(?i)\b(kernel\s+exploit|kernel\s+module\s+rootkit|dirty\s+cow|privilege\s+escalation\s+via\s+kernel)\b|内核漏洞|内核提权`, Weight: 60, Category: "exploit", Strict: true},
	{Name: "zero_click_exploit", Pattern: `(?i)\b(zero[-\s]?click\s+exploit|remote\s+code\s+execution\s+without\s+interaction|wormable\s+exploit)\b|零点击漏洞`, Weight: 70, Category: "exploit", Strict: true},
	{Name: "sandbox_escape", Pattern: `(?i)\b(sandbox\s+escape|vm\s+escape|browser\s+sandbox\s+bypass|jvm\s+sandbox\s+escape)\b|沙箱逃逸|虚拟机逃逸`, Weight: 55, Category: "exploit"},
	{Name: "firmware_backdoor", Pattern: `(?i)\b(firmware\s+backdoor|bios\s+rootkit|uefi\s+malware|bootkit)\b|固件后门|bios木马`, Weight: 75, Category: "malware", Strict: true},
	{Name: "supply_chain_backdoor", Pattern: `(?i)\b(backdoor.{0,40}(npm|pypi|rubygems|maven)|trojanized\s+package|malicious\s+dependency)\b|依赖后门|恶意包`, Weight: 70, Category: "supply_chain", Strict: true},
	{Name: "credential_dumping", Pattern: `(?i)\b(lsass\s+dump|sam\s+dump|ntds\.dit|credential\s+dumping|hashdump)\b|凭证转储|密码哈希导出`, Weight: 65, Category: "credential_attack", Strict: true},
	{Name: "lateral_movement", Pattern: `(?i)\b(lateral\s+movement|pass[-\s]?the[-\s]?hash|pass[-\s]?the[-\s]?ticket|psexec|wmi\s+exec)\b|横向移动`, Weight: 50, Category: "post_exploitation"},
	{Name: "domain_takeover", Pattern: `(?i)\b(domain\s+takeover|subdomain\s+hijacking|dns\s+takeover|dangling\s+cname)\b|域名劫持|子域接管`, Weight: 55, Category: "network_attack"},
	{Name: "token_theft", Pattern: `(?i)\b(oauth\s+token\s+theft|jwt\s+hijacking|bearer\s+token\s+steal|access\s+token\s+exfiltration)\b|token窃取|令牌劫持`, Weight: 60, Category: "credential_attack", Strict: true},
	{Name: "process_injection", Pattern: `(?i)\b(process\s+injection|dll\s+injection|reflective\s+loading|process\s+hollowing)\b|进程注入|dll注入`, Weight: 55, Category: "evasion"},
	{Name: "fileless_malware", Pattern: `(?i)\b(fileless\s+malware|living\s+off\s+the\s+land|lolbins|powershell\s+empire)\b|无文件攻击`, Weight: 50, Category: "evasion"},
	{Name: "log_tampering", Pattern: `(?i)\b(log\s+deletion|event\s+log\s+clearing|anti[-\s]?forensics|cover\s+tracks)\b|日志清除|反取证`, Weight: 45, Category: "evasion"},
	{Name: "vpn_proxy_abuse", Pattern: `(?i)\b(vpn\s+exploit|proxy\s+chain\s+for\s+anonymity|tor\s+hidden\s+service\s+setup)\b|vpn漏洞|代理链匿名`, Weight: 30, Category: "evasion"},
	{Name: "database_dump", Pattern: `(?i)\b(database\s+dump|mysqldump\s+attack|mongodb\s+ransom|elasticsearch\s+exposure)\b|数据库导出|数据库勒索`, Weight: 55, Category: "data_theft"},
	{Name: "api_key_scraping", Pattern: `(?i)\b(scrape\s+api\s+keys|github\s+secret\s+scanning|hardcoded\s+credentials\s+search)\b|api密钥爬取|硬编码凭证搜索`, Weight: 50, Category: "credential_attack"},
	{Name: "mass_exploitation", Pattern: `(?i)\b(mass\s+exploitation|automated\s+exploitation|exploit\s+at\s+scale|worm\s+propagation)\b|大规模利用|蠕虫传播`, Weight: 70, Category: "exploit", Strict: true},
	{Name: "insider_threat", Pattern: `(?i)\b(insider\s+threat|rogue\s+employee|data\s+theft\s+by\s+employee|sabotage)\b|内部威胁|员工窃密`, Weight: 40, Category: "data_theft"},
	{Name: "cryptographic_attack", Pattern: `(?i)\b(padding\s+oracle|timing\s+attack|side[-\s]?channel\s+attack|weak\s+encryption\s+exploit)\b|密码学攻击|侧信道攻击`, Weight: 35, Category: "crypto_attack"},
	{Name: "race_condition_exploit", Pattern: `(?i)\b(race\s+condition\s+exploit|toctou|time[-\s]?of[-\s]?check\s+time[-\s]?of[-\s]?use)\b|竞态条件漏洞`, Weight: 30, Category: "exploit"},
	{Name: "hardware_implant", Pattern: `(?i)\b(hardware\s+implant|usb\s+rubber\s+ducky|malicious\s+usb|hardware\s+keylogger)\b|硬件植入|恶意usb`, Weight: 60, Category: "physical_attack", Strict: true},
	{Name: "social_media_hijack", Pattern: `(?i)\b(account\s+takeover|social\s+media\s+hijacking|credential\s+stuffing)\b|账号接管|撞库攻击`, Weight: 40, Category: "credential_attack"},
	{Name: "anti_bot_challenge_bypass", Pattern: `(?i)\b(bypass|evade|defeat|crack|solve|automate)\b.{0,80}\b(captcha|recaptcha|hcaptcha|cloudflare|waf|anti[-\s]?bot|slider\s+challenge)\b|(?:绕过|破解|自动解|规避).{0,40}(验证码|滑块|极验|vaptcha|cloudflare|waf|反爬)`, Weight: 90, Category: "security_bypass", Strict: true},
	{Name: "batch_account_abuse", Pattern: `(?i)\b(batch|bulk|mass|automated|at\s+scale)\b.{0,60}(?:\b(register|create|farm|warm\s+up|maintain)\b.{0,50}\b(accounts?|profiles?)\b|\b(accounts?|profiles?)\b.{0,40}\b(registration|creation|farming|warming|maintenance)\b)|(?:批量|自动).{0,30}(注册|养号|创建).{0,20}(账号|账户)`, Weight: 90, Category: "account_abuse", Strict: true},
	{Name: "fake_engagement_automation", Pattern: `(?i)\b(bot|automate|automation|script)\b.{0,60}\b(fake\s+(reviews?|likes?|followers?|orders?|traffic)|review\s+bombing|click\s+farm)\b|(?:机器人|脚本|自动).{0,40}(刷单|刷量|控评|刷赞|刷粉|虚假流量)`, Weight: 85, Category: "platform_abuse", Strict: true},
	{Name: "mass_phishing_or_scam", Pattern: `(?i)\b(mass|bulk|automated|campaign)\b.{0,60}\b(phishing|scam|fraud)\b.{0,60}\b(send|message|email|dm|broadcast)\b|(?:批量|群发|自动).{0,40}(钓鱼|诈骗|欺诈).{0,20}(邮件|消息|短信|私信)`, Weight: 100, Category: "social_engineering", Strict: true},
	{Name: "abusive_account_token_pool", Pattern: `(?i)\b(stolen|harvested|compromised|other\s+people'?s)\b.{0,60}\b(accounts?|tokens?|api\s+keys?)\b.{0,60}\b(pool|rotation|rotate|proxy)\b|\b(pool|rotation|rotate|proxy)\b.{0,60}\b(stolen|harvested|compromised|other\s+people'?s)\b.{0,60}\b(accounts?|tokens?|api\s+keys?)\b|(?:盗取|窃取|他人|黑产).{0,30}(账号|账户|token|令牌|api\s*key).{0,30}(池|轮换|资源池)`, Weight: 100, Category: "account_abuse", Strict: true},
	{Name: "adult_deepfake", Pattern: `(?i)\b(create|make|generate|produce|edit|render|show|provide)\b.{0,100}(?:\b(deepfake|face\s*swap)\b.{0,80}\b(porn|nude|naked|sexual|explicit)\b|\b(porn|nude|naked|sexual|explicit)\b.{0,80}\b(deepfake|face\s*swap)\b)|(?:制作|生成|创建|编辑|提供|给我).{0,50}(?:换脸|深度伪造|deepfake).{0,40}(色情|成人|裸体|裸照|不雅)`, Weight: 100, Category: "deepfake_adult", Strict: true},
	{Name: "doxing_personal_data", Pattern: `(?i)\b(?:dox|doxx)\b.{0,80}\b(someone|another\s+person|other\s+people|him|her|them|that\s+person|this\s+person)\b.{0,40}\b(home\s+address|phone\s+number|private\s+address|personal\s+information|identity\s+details)\b|\b(expose|publish|leak)\b.{0,80}\b(someone'?s|another\s+person'?s|other\s+people'?s|his|her|their|that\s+person'?s|this\s+person'?s)\b.{0,40}\b(home\s+address|phone\s+number|private\s+address|personal\s+information|identity\s+details)\b|\b(find|locate)\b.{0,60}\b(someone'?s|another\s+person'?s|other\s+people'?s|his|her|their|that\s+person'?s|this\s+person'?s)\b.{0,40}\b(home\s+address|phone\s+number|private\s+address|personal\s+information|identity\s+details)\b|(?:^|[.!?。！？]\s*)(?:请)?(?:帮我|我要|我想|给我|去|准备|计划)?\s*(?:人肉|开盒).{0,30}(他人|别人|某人|他|她).{0,30}(住址|家庭地址|手机号|身份证|真实身份|个人隐私)|(?:^|[.!?。！？]\s*)(?:请)?(?:帮我|我要|我想|给我|去|准备|计划)?\s*(?:查找|曝光|泄露).{0,30}(他人|别人|某人|他的|她的|他们的).{0,30}(住址|家庭地址|手机号|身份证|真实身份|个人隐私)`, Weight: 100, Category: "doxing", Strict: true},
	{Name: "real_person_violent_threat", Pattern: `(?i)\b(i\s+will|i\s+am\s+going\s+to|i'm\s+going\s+to|we\s+will|threaten\s+to)\b.{0,40}\b(kill|shoot|stab|beat|hurt|attack)\b.{0,40}\b(him|her|them|someone|that\s+person|this\s+person|my\s+(?:boss|neighbor|teacher|coworker|family))\b|(?:我要|我会|准备|威胁要).{0,20}(杀|枪击|捅|殴打|伤害|袭击).{0,20}(他|她|他们|那个人|这个人|真人)`, Weight: 100, Category: "violent_threat", Strict: true},
}

var contentModerationBuiltinRegexDefensiveContexts = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(defensive|defense|prevent|prevention|mitigation|detect|detection|hardening|patch|remediation|incident\s+response)\b`),
	regexp.MustCompile(`(?i)\b(do\s+not\s+provide|without\s+code|no\s+commands|high\s+level|non[-\s]?operational|refusal|unsafe)\b`),
	regexp.MustCompile(`(?i)\b(what\s+(?:is|are)|explain|overview|definition|history|educational|conceptual|translate|summarize|news|report|documentation)\b`),
	regexp.MustCompile(`防御|修复|检测|加固|不要提供|不提供代码`),
	regexp.MustCompile(`什么是|是什么|解释|介绍|概念|原理|历史|翻译|总结|新闻|报告|文档`),
}

var contentModerationBuiltinRegexAuthorizedContext = regexp.MustCompile(`(?i)\b(my\s+own|our\s+own|i\s+own|we\s+own|authorized|with\s+(?:explicit\s+)?permission|ctf|capture\s+the\s+flag|lab\s+environment)\b|\b(?:my|our)\s+(?:system|server|application|app|account|deployment|repository|repo|codebase|network|database|website|service|infrastructure|device)\b|我自己的|我们自己的|自有系统|已授权|授权测试|靶场|(?:我的|我们的)(?:项目|系统|服务器|账号|账户|部署|代码|仓库|网络|数据库|网站|服务|设备)`)

var contentModerationBuiltinRegexOperationalContext = regexp.MustCompile(`(?i)\b(write|generate|create|build|craft|deploy|steal|bypass|crack|hack)\b|\bhow\s+to\s+(steal|bypass|crack|attack|exploit|hack)\b|(?:帮我|给我|请|如何|怎么|我要|我想|我们要).{0,12}(写|生成|构造|制作|部署|窃取|绕过|破解|攻击|入侵)`)

var contentModerationBuiltinRegexLeetspeakReplacer = strings.NewReplacer(
	"0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "@", "a", "$", "s",
)

type contentModerationBuiltinRegexRule struct {
	definition ContentModerationRegexRule
	regexp     *regexp.Regexp
}

type contentModerationBuiltinRegexMatcher struct {
	rules           []contentModerationBuiltinRegexRule
	threshold       int
	strictThreshold int
}

type contentModerationBuiltinRegexMatch struct {
	Name     string
	Weight   int
	Category string
	Strict   bool
}

type contentModerationBuiltinRegexVerdict struct {
	Blocked         bool
	Score           int
	RawScore        int
	StrictScore     int
	ContextDiscount int
	HighestCategory string
	CategoryScores  map[string]float64
	Matches         []contentModerationBuiltinRegexMatch
	Reason          string
}

func compileContentModerationBuiltinRegexRules(definitions []ContentModerationRegexRule) []contentModerationBuiltinRegexRule {
	rules := make([]contentModerationBuiltinRegexRule, 0, len(definitions))
	for _, definition := range definitions {
		compiled, err := regexp.Compile(definition.Pattern)
		if err != nil {
			continue
		}
		rules = append(rules, contentModerationBuiltinRegexRule{
			definition: definition,
			regexp:     compiled,
		})
	}
	return rules
}

func newContentModerationBuiltinRegexMatcher(cfg *ContentModerationConfig) *contentModerationBuiltinRegexMatcher {
	if cfg == nil || !cfg.BuiltinRegexEnabled {
		return nil
	}
	definitions := filterDisabledContentModerationBuiltinRegexRules(cfg.BuiltinRegexRules, cfg.DisabledBuiltinRegexRules)
	rules := compileContentModerationBuiltinRegexRules(definitions)
	if len(rules) == 0 {
		return nil
	}
	return &contentModerationBuiltinRegexMatcher{
		rules:           rules,
		threshold:       cfg.BuiltinRegexThreshold,
		strictThreshold: cfg.BuiltinRegexStrictThreshold,
	}
}

func (m *contentModerationBuiltinRegexMatcher) Match(text string) (contentModerationBuiltinRegexVerdict, bool) {
	verdict := contentModerationBuiltinRegexVerdict{CategoryScores: map[string]float64{}}
	if m == nil || len(m.rules) == 0 || strings.TrimSpace(text) == "" {
		return verdict, false
	}
	scanText := normalizeContentModerationBuiltinRegexText(text)
	obfuscatedScanText := deobfuscateContentModerationBuiltinRegexText(scanText)
	categoryWeights := map[string]int{}
	strictCategoryWeights := map[string]int{}
	for _, rule := range m.rules {
		if !rule.regexp.MatchString(scanText) && (obfuscatedScanText == scanText || !rule.regexp.MatchString(obfuscatedScanText)) {
			continue
		}
		definition := rule.definition
		verdict.Matches = append(verdict.Matches, contentModerationBuiltinRegexMatch{
			Name: definition.Name, Weight: definition.Weight, Category: definition.Category, Strict: definition.Strict,
		})
		verdict.RawScore += definition.Weight
		if definition.Weight > categoryWeights[definition.Category] {
			categoryWeights[definition.Category] = definition.Weight
		}
		if definition.Strict && definition.Weight > strictCategoryWeights[definition.Category] {
			strictCategoryWeights[definition.Category] = definition.Weight
		}
	}
	if len(verdict.Matches) == 0 {
		return verdict, false
	}
	sort.Slice(verdict.Matches, func(i, j int) bool {
		if verdict.Matches[i].Weight == verdict.Matches[j].Weight {
			return verdict.Matches[i].Name < verdict.Matches[j].Name
		}
		return verdict.Matches[i].Weight > verdict.Matches[j].Weight
	})
	baseScore := 0
	discountableScore := 0
	for category, weight := range categoryWeights {
		baseScore += weight
		if contentModerationBuiltinRegexCategoryAllowsContextDiscount(category) {
			discountableScore += weight
		}
	}
	strictDiscountableScore := 0
	for category, weight := range strictCategoryWeights {
		verdict.StrictScore += weight
		if contentModerationBuiltinRegexCategoryAllowsContextDiscount(category) {
			strictDiscountableScore += weight
		}
	}
	contextDiscount := contentModerationBuiltinRegexDefensiveDiscount(scanText)
	verdict.ContextDiscount = min(contextDiscount, discountableScore)
	verdict.Score = baseScore - verdict.ContextDiscount
	if verdict.Score < 0 {
		verdict.Score = 0
	}
	verdict.StrictScore -= min(contextDiscount, strictDiscountableScore)
	if verdict.StrictScore < 0 {
		verdict.StrictScore = 0
	}
	for category, weight := range categoryWeights {
		verdict.CategoryScores[category] = normalizedContentModerationRegexScore(weight)
		if verdict.HighestCategory == "" || weight > categoryWeights[verdict.HighestCategory] || (weight == categoryWeights[verdict.HighestCategory] && category < verdict.HighestCategory) {
			verdict.HighestCategory = category
		}
	}
	verdict.Blocked = verdict.Score >= m.threshold || verdict.StrictScore >= m.strictThreshold
	verdict.Reason = contentModerationBuiltinRegexReason(verdict, m.threshold, m.strictThreshold)
	return verdict, true
}

func normalizeContentModerationBuiltinRegexText(text string) string {
	text = norm.NFKC.String(text)
	text = strings.ReplaceAll(text, "```", " ")
	text = strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cf) {
			return -1
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ' '
		}
		return unicode.ToLower(r)
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

func deobfuscateContentModerationBuiltinRegexText(text string) string {
	return contentModerationBuiltinRegexLeetspeakReplacer.Replace(text)
}

func contentModerationBuiltinRegexDefensiveDiscount(text string) int {
	if contentModerationBuiltinRegexAuthorizedContext.MatchString(text) {
		return maxContentModerationBuiltinRegexStrictThreshold
	}
	if contentModerationBuiltinRegexOperationalContext.MatchString(text) {
		return 0
	}
	discount := 0
	for _, pattern := range contentModerationBuiltinRegexDefensiveContexts {
		if pattern.MatchString(text) {
			discount += 60
		}
	}
	if discount > 90 {
		return 90
	}
	return discount
}

func contentModerationBuiltinRegexCategoryAllowsContextDiscount(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "deepfake_adult", "doxing", "violent_threat":
		return false
	default:
		return true
	}
}

func contentModerationBuiltinRegexReason(verdict contentModerationBuiltinRegexVerdict, threshold, strictThreshold int) string {
	names := make([]string, 0, len(verdict.Matches))
	for _, match := range verdict.Matches {
		names = append(names, match.Name)
	}
	return fmt.Sprintf("builtin regex score=%d/%d strict=%d/%d rules=%s", verdict.Score, threshold, verdict.StrictScore, strictThreshold, strings.Join(names, ","))
}

func normalizedContentModerationRegexScore(score int) float64 {
	if score <= 0 {
		return 0
	}
	if score >= 100 {
		return 1
	}
	return float64(score) / 100
}

func contentModerationBuiltinRegexMatchedRules(matches []contentModerationBuiltinRegexMatch) string {
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.Name)
	}
	return strings.Join(names, ",")
}

func contentModerationBuiltinRegexRuleNames(rules []ContentModerationRegexRule) []string {
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return names
}

func defaultContentModerationBuiltinRegexRules() []ContentModerationRegexRule {
	return cloneContentModerationBuiltinRegexRules(contentModerationBuiltinRegexRuleDefinitions)
}

func upgradeLegacyContentModerationBuiltinRegexRules(rules []ContentModerationRegexRule, storedVersion int) []ContentModerationRegexRule {
	if len(rules) == 0 {
		return rules
	}
	upgraded := cloneContentModerationBuiltinRegexRules(rules)
	legacyNames := make(map[string]struct{}, legacyContentModerationBuiltinRegexRuleCount)
	for _, definition := range contentModerationBuiltinRegexRuleDefinitions[:legacyContentModerationBuiltinRegexRuleCount] {
		legacyNames[strings.ToLower(definition.Name)] = struct{}{}
	}
	configuredNames := make(map[string]struct{}, len(upgraded))
	hasLegacyRule := false
	for index := range upgraded {
		rule := &upgraded[index]
		name := strings.ToLower(strings.TrimSpace(rule.Name))
		configuredNames[name] = struct{}{}
		if _, ok := legacyNames[name]; ok {
			hasLegacyRule = true
		}
		switch name {
		case "remote_shell":
			if rule.Weight == 45 && rule.Pattern == `(?i)\b(reverse\s+shell|bind\s+shell|meterpreter|web\s+shell)\b|反弹\s*shell` {
				rule.Weight = 55
			}
		case "operational_exploit_request":
			if rule.Pattern == `(?i)\b(write|generate|create|give|build|craft|make)\b.{0,80}\b(exploit|payload|poc|proof[-\s]?of[-\s]?concept|0day|zero[-\s]?day)\b|(?:写|生成|给我|构造|制作).{0,40}(漏洞利用|攻击载荷|payload|poc)` {
				rule.Pattern = `(?i)\b(write|generate|create|give|build|craft|make)\b.{0,80}\b(exploit(?:\s+payload)?|payload\s+(?:for|to)\s+exploit|poc|proof[-\s]?of[-\s]?concept|0day|zero[-\s]?day)\b|(?:写|生成|给我|构造|制作).{0,40}(漏洞利用|攻击载荷|payload|poc)`
			}
		case "generic_exploit":
			if rule.Pattern == `(?i)\b(exploit|payload|vulnerability|0day|zero[-\s]?day)\b` {
				rule.Pattern = `(?i)\b(exploit|vulnerability|0day|zero[-\s]?day)\b`
			}
		case "adult_deepfake":
			if rule.Pattern == `(?i)\b(deepfake|face\s*swap|ai[-\s]?generated)\b.{0,80}\b(porn|nude|naked|sexual|explicit)\b|\b(porn|nude|naked|sexual|explicit)\b.{0,80}\b(deepfake|face\s*swap)\b|(?:换脸|深度伪造|deepfake).{0,40}(色情|成人|裸体|裸照|不雅)` {
				rule.Pattern = contentModerationBuiltinRegexRuleDefinitionByName("adult_deepfake").Pattern
			}
		case "doxing_personal_data":
			if rule.Pattern == `(?i)\b(doxx?|doxing|expose|publish|find|leak)\b.{0,80}\b(home\s+address|phone\s+number|private\s+address|personal\s+information|identity\s+details)\b|(?:人肉|开盒|查找|曝光|泄露).{0,40}(住址|家庭地址|手机号|身份证|真实身份|个人隐私)` {
				rule.Pattern = contentModerationBuiltinRegexRuleDefinitionByName("doxing_personal_data").Pattern
			}
		}
	}
	if !hasLegacyRule || storedVersion >= 1 {
		return upgraded
	}
	for _, definition := range contentModerationBuiltinRegexRuleDefinitions[legacyContentModerationBuiltinRegexRuleCount:] {
		if _, exists := configuredNames[strings.ToLower(definition.Name)]; exists {
			continue
		}
		upgraded = append(upgraded, definition)
	}
	return upgraded
}

func contentModerationBuiltinRegexRuleDefinitionByName(name string) ContentModerationRegexRule {
	for _, definition := range contentModerationBuiltinRegexRuleDefinitions {
		if definition.Name == name {
			return definition
		}
	}
	return ContentModerationRegexRule{}
}

func cloneContentModerationBuiltinRegexRules(rules []ContentModerationRegexRule) []ContentModerationRegexRule {
	if rules == nil {
		return nil
	}
	out := make([]ContentModerationRegexRule, len(rules))
	copy(out, rules)
	return out
}

func normalizeContentModerationBuiltinRegexRules(rules []ContentModerationRegexRule) []ContentModerationRegexRule {
	if rules == nil {
		return nil
	}
	out := make([]ContentModerationRegexRule, len(rules))
	for index, rule := range rules {
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		rule.Category = strings.TrimSpace(rule.Category)
		out[index] = rule
	}
	return out
}

func validateContentModerationBuiltinRegexRules(rules []ContentModerationRegexRule) error {
	if len(rules) > maxContentModerationBuiltinRegexRules {
		return fmt.Errorf("本地正则规则不能超过 %d 条", maxContentModerationBuiltinRegexRules)
	}
	seen := make(map[string]struct{}, len(rules))
	for index, rule := range rules {
		position := index + 1
		if rule.Name == "" {
			return fmt.Errorf("第 %d 条本地正则规则名称不能为空", position)
		}
		if len([]rune(rule.Name)) > maxContentModerationBuiltinRegexRuleNameRunes || strings.IndexFunc(rule.Name, unicode.IsControl) >= 0 {
			return fmt.Errorf("规则 %q 的名称无效", rule.Name)
		}
		key := strings.ToLower(rule.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("本地正则规则名称重复: %s", rule.Name)
		}
		seen[key] = struct{}{}
		if rule.Category == "" || len([]rune(rule.Category)) > maxContentModerationBuiltinRegexCategoryRunes || strings.IndexFunc(rule.Category, unicode.IsControl) >= 0 {
			return fmt.Errorf("规则 %q 的分类无效", rule.Name)
		}
		if rule.Weight <= 0 || rule.Weight > maxContentModerationBuiltinRegexRuleWeight {
			return fmt.Errorf("规则 %q 的权重必须在 1-%d 之间", rule.Name, maxContentModerationBuiltinRegexRuleWeight)
		}
		if rule.Pattern == "" || len([]rune(rule.Pattern)) > maxContentModerationBuiltinRegexPatternRunes {
			return fmt.Errorf("规则 %q 的正则表达式为空或过长", rule.Name)
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("规则 %q 的正则表达式无效: %v", rule.Name, err)
		}
	}
	return nil
}

func filterDisabledContentModerationBuiltinRegexRules(rules []ContentModerationRegexRule, disabledNames []string) []ContentModerationRegexRule {
	if len(rules) == 0 || len(disabledNames) == 0 {
		return cloneContentModerationBuiltinRegexRules(rules)
	}
	disabled := make(map[string]struct{}, len(disabledNames))
	for _, name := range disabledNames {
		disabled[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	out := make([]ContentModerationRegexRule, 0, len(rules))
	for _, rule := range rules {
		if _, off := disabled[strings.ToLower(rule.Name)]; !off {
			out = append(out, rule)
		}
	}
	return out
}

func normalizeDisabledContentModerationBuiltinRegexRules(names []string) []string {
	canonical := make(map[string]string, len(contentModerationBuiltinRegexRuleDefinitions))
	for _, rule := range contentModerationBuiltinRegexRuleDefinitions {
		canonical[strings.ToLower(rule.Name)] = rule.Name
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name, ok := canonical[strings.ToLower(strings.TrimSpace(raw))]
		if !ok {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
