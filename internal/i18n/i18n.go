package i18n

import (
	"os"
	"strings"
	"sync/atomic"
)

const (
	LanguageAuto    = "auto"
	LanguageEnglish = "en"
	LanguageChinese = "zh-CN"
)

type Locale string

const (
	English Locale = "en"
	Chinese Locale = "zh-CN"
)

var current atomic.Value

func init() { current.Store(English) }

func Resolve(language string) Locale {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "zh", "zh-cn", "zh_cn", "chinese":
		return Chinese
	case "en", "en-us", "en-gb", "english":
		return English
	case "auto", "":
		for _, key := range []string{"LC_ALL", "LANG", "LANGUAGE"} {
			value := strings.ToLower(os.Getenv(key))
			if strings.HasPrefix(value, "zh") {
				return Chinese
			}
		}
	}
	return English
}

func SetCurrent(language string) Locale {
	locale := Resolve(language)
	current.Store(locale)
	return locale
}

func Current() Locale { return current.Load().(Locale) }

func T(text string) string { return Translate(Current(), text) }

func Translate(locale Locale, text string) string {
	if locale != Chinese {
		return text
	}
	if translated, ok := chinese[text]; ok {
		return translated
	}
	return text
}

var chinese = map[string]string{
	"Artists": "艺术家", "Albums": "专辑", "Playlists": "歌单", "Search": "搜索",
	"Playing": "正在播放", "System": "系统", "Controls": "操作", "Queue": "播放队列",
	"Help": "帮助", "Command Palette": "命令面板", "Now Playing": "正在播放",
	"Login": "登录", "Quit": "退出", "Ready": "就绪", "Status": "状态",
	"Username": "用户名", "Password": "密码", "Password mode": "密码模式",
	"Token/salt": "令牌/盐", "Plaintext": "明文",
	"Cached": "已缓存", "Download": "下载", "Downloaded": "已下载", "Offline": "离线",
	"Buffering": "缓冲中", "Stream": "流媒体",
	"Online": "在线", "Retry Online": "重试在线", "Sync Library": "同步资料库",
	"Library synced": "资料库已同步", "Download queued": "已加入下载队列",
	"Download removed": "已删除下载", "Syncing library...": "正在同步资料库…",
	"Download complete": "下载完成", "Cache complete": "缓存完成",
	"Retrying online...": "正在重试在线连接…", "Go Offline": "切换到离线",
	"Connection mode": "连接模式", "Auto": "自动", "Language": "语言",
	"English": "English", "中文": "中文", "Endpoints": "端点",
	"Audio backend": "音频后端", "MPV path": "MPV 路径", "Cache dir": "缓存目录",
	"Audio cache MB": "音频缓存 MB", "Health interval sec": "健康检查间隔（秒）",
	"Switch threshold": "切换阈值", "Prefetch": "预取", "Bundled mpv": "内置 mpv",
	"Catalog sync sec": "资料库同步间隔（秒）", "Download concurrency": "下载并发数",
	"Offline storage MB": "离线存储 MB", "Download variant": "下载版本",
	"Sync library": "同步资料库", "Retry online": "重试在线",
	"Raw": "原始文件", "On": "开", "Off": "关",
	"Unlimited": "不限", "Press Enter": "按回车", "PATH lookup": "从 PATH 查找",
	"Save": "保存", "Cancel": "取消", "F2 save | Esc close": "F2 保存 | Esc 关闭",
	"OK": "确定", "Choose ": "选择",
	"Waiting to ping.": "等待检测。", "Offline mode; endpoint ping skipped.": "离线模式，跳过 Endpoint 检测。",
	"Global commands": "全局命令", "Navigation": "导航", "Playback": "播放",
	"Space Play/Pause": "空格 播放/暂停",
	"Play/Pause":       "播放/暂停", "Previous Track": "上一曲", "Next Track": "下一曲",
	"Toggle Repeat": "切换循环", "Shuffle": "随机播放", "Seek Back 10s": "后退 10 秒",
	"Seek Forward 10s": "前进 10 秒", "Volume Down 5%": "音量降低 5%", "Volume Up 5%": "音量提高 5%",
	"Volume Down 1%": "音量降低 1%", "Volume Up 1%": "音量提高 1%",
	"Download Current Track": "下载当前曲目", "Remove Current Download": "删除当前下载",
	"Esc / Backspace / ? / ？ close": "Esc / Backspace / ? / ？ 关闭",
	"No track loaded":               "没有加载曲目", "Endpoint: ": "Endpoint：",
	"Start playback from an album, playlist, or search result.": "从专辑、歌单或搜索结果开始播放。",
	"Press Esc to return.": "按 Esc 返回。", "Now Playing needs a different terminal shape.": "正在播放需要不同的终端布局。",
	"Resize the terminal or press Esc to return.": "调整终端大小或按 Esc 返回。", "Error: ": "错误：",
	"Play": "播放", "Pause": "暂停", "Prev": "上一曲", "Next": "下一曲",
	"Repeat: ": "循环：", "Shuffle: ": "随机：", "Rep:": "循环：", "Shuf:": "随机：",
	"Unknown":                  "未知",
	"Opening local library...": "正在打开本地资料库…", "Connecting to server...": "正在连接服务器…",
	"Loading artists...": "正在加载艺术家…", "Loading artist...": "正在加载艺术家…",
	"Loading albums...": "正在加载专辑…", "Loading album...": "正在加载专辑…",
	"Loading playlists...": "正在加载歌单…", "Loading playlist...": "正在加载歌单…",
	"Type a query and press Enter.": "输入查询并按回车。", "Searching...": "正在搜索…",
}
