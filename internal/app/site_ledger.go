package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

// 网站排行账本：核心在连接关闭时写一条 JSON 记录，面板增量聚合到每天一个
// 文件、只留 31 天。总量不单独计数，由明细求和得出；现有 v2ray 总量统计
// 不动，两套账各司其职。

// siteRecord 是核心 site_log 写出的一行。
type siteRecord struct {
	User     string `json:"user"`
	Host     string `json:"host"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
}

// siteTotals 是一天内某用户在某服务上的累计：双向流量 + 连接存活时间之和。
type siteTotals struct {
	Upload   int64 `json:"up"`
	Download int64 `json:"down"`
	Duration int64 `json:"dur"`
}

func (t *siteTotals) bytes() int64 { return t.Upload + t.Download }

// daySiteLedger 是一天的账本：用户 -> 服务 -> 累计。
type daySiteLedger struct {
	Date     string                            `json:"date"`
	Services map[string]map[string]*siteTotals `json:"services"`
}

func newDaySiteLedger(date string) *daySiteLedger {
	return &daySiteLedger{Date: date, Services: map[string]map[string]*siteTotals{}}
}

// siteService 是域名后缀到服务的映射：一个品牌一个服务，域名行显示的域名
// 是该服务的代表域名。
type siteService struct {
	suffixes []string
	id       string
	name     string
	domain   string
	icon     string
}

var siteServices = []siteService{
	{[]string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtube-nocookie.com"}, "youtube", "YouTube", "youtube.com", "youtube"},
	{[]string{"netflix.com", "nflxext.com", "nflximg.net", "nflxvideo.net"}, "netflix", "Netflix", "netflix.com", "netflix"},
	{[]string{"twitch.tv", "twitchcdn.net"}, "twitch", "Twitch", "twitch.tv", "twitch"},
	{[]string{"tiktok.com", "tiktokcdn.com"}, "tiktok", "TikTok", "tiktok.com", "tiktok"},
	{[]string{"vimeo.com"}, "vimeo", "Vimeo", "vimeo.com", "vimeo"},
	{[]string{"dailymotion.com"}, "dailymotion", "Dailymotion", "dailymotion.com", "dailymotion"},
	{[]string{"crunchyroll.com"}, "crunchyroll", "Crunchyroll", "crunchyroll.com", "crunchyroll"},
	{[]string{"max.com", "hbomax.com"}, "hbomax", "HBO Max", "max.com", "hbomax"},
	{[]string{"tv.apple.com"}, "appletv", "Apple TV+", "tv.apple.com", "appletv"},
	{[]string{"paramountplus.com"}, "paramountplus", "Paramount+", "paramountplus.com", "paramountplus"},
	{[]string{"tubitv.com"}, "tubi", "Tubi", "tubitv.com", "tubi"},
	{[]string{"plex.tv"}, "plex", "Plex", "plex.tv", "plex"},
	{[]string{"kodi.tv"}, "kodi", "Kodi", "kodi.tv", "kodi"},
	{[]string{"viki.com"}, "viki", "Viki", "viki.com", "viki"},
	{[]string{"rumble.com"}, "rumble", "Rumble", "rumble.com", "rumble"},
	{[]string{"odysee.com"}, "odysee", "Odysee", "odysee.com", "odysee"},
	{[]string{"kick.com"}, "kick", "Kick", "kick.com", "kick"},
	{[]string{"vevo.com"}, "vevo", "Vevo", "vevo.com", "vevo"},
	{[]string{"disneyplus.com", "disney-plus.net", "disney.com"}, "disney", "Disney+", "disneyplus.com", "disney"},
	{[]string{"hulu.com"}, "hulu", "Hulu", "hulu.com", "hulu"},
	{[]string{"primevideo.com", "amazonvideo.com"}, "prime", "Prime Video", "primevideo.com", "prime"},
	{[]string{"peacocktv.com"}, "peacock", "Peacock", "peacocktv.com", "peacock"},
	{[]string{"pluto.tv"}, "plutotv", "Pluto TV", "pluto.tv", "plutotv"},
	{[]string{"pornhub.com"}, "pornhub", "Pornhub", "pornhub.com", "pornhub"},
	{[]string{"xvideos.com"}, "xvideos", "XVideos", "xvideos.com", "xvideos"},
	{[]string{"xnxx.com"}, "xnxx", "XNXX", "xnxx.com", "xnxx"},
	{[]string{"redtube.com"}, "redtube", "RedTube", "redtube.com", "redtube"},
	{[]string{"youporn.com"}, "youporn", "YouPorn", "youporn.com", "youporn"},
	{[]string{"brazzers.com"}, "brazzers", "Brazzers", "brazzers.com", "brazzers"},
	{[]string{"onlyfans.com"}, "onlyfans", "OnlyFans", "onlyfans.com", "onlyfans"},
	{[]string{"facebook.com", "fbcdn.net", "fb.com"}, "facebook", "Facebook", "facebook.com", "facebook"},
	{[]string{"instagram.com", "cdninstagram.com"}, "instagram", "Instagram", "instagram.com", "instagram"},
	{[]string{"x.com", "twitter.com", "twimg.com"}, "x", "X", "x.com", "x"},
	{[]string{"whatsapp.com", "whatsapp.net"}, "whatsapp", "WhatsApp", "whatsapp.com", "whatsapp"},
	{[]string{"telegram.org", "t.me"}, "telegram", "Telegram", "telegram.org", "telegram"},
	{[]string{"discord.com"}, "discord", "Discord", "discord.com", "discord"},
	{[]string{"reddit.com"}, "reddit", "Reddit", "reddit.com", "reddit"},
	{[]string{"pinterest.com"}, "pinterest", "Pinterest", "pinterest.com", "pinterest"},
	{[]string{"snapchat.com"}, "snapchat", "Snapchat", "snapchat.com", "snapchat"},
	{[]string{"signal.org"}, "signal", "Signal", "signal.org", "signal"},
	{[]string{"threads.net"}, "threads", "Threads", "threads.net", "threads"},
	{[]string{"linkedin.com"}, "linkedin", "LinkedIn", "linkedin.com", "linkedin"},
	{[]string{"skype.com"}, "skype", "Skype", "skype.com", "skype"},
	{[]string{"messenger.com"}, "messenger", "Messenger", "messenger.com", "messenger"},
	{[]string{"tumblr.com"}, "tumblr", "Tumblr", "tumblr.com", "tumblr"},
	{[]string{"line.me"}, "line", "LINE", "line.me", "line"},
	{[]string{"mastodon.social"}, "mastodon", "Mastodon", "mastodon.social", "mastodon"},
	{[]string{"bsky.app"}, "bluesky", "Bluesky", "bsky.app", "bluesky"},
	{[]string{"kakao.com"}, "kakao", "KakaoTalk", "kakao.com", "kakao"},
	{[]string{"viber.com"}, "viber", "Viber", "viber.com", "viber"},
	{[]string{"badoo.com"}, "badoo", "Badoo", "badoo.com", "badoo"},
	{[]string{"gab.com"}, "gab", "Gab", "gab.com", "gab"},
	{[]string{"parler.com"}, "parler", "Parler", "parler.com", "parler"},
	{[]string{"truthsocial.com"}, "truthsocial", "Truth Social", "truthsocial.com", "truthsocial"},
	{[]string{"clubhouse.com"}, "clubhouse", "Clubhouse", "clubhouse.com", "clubhouse"},
	{[]string{"openai.com", "chatgpt.com"}, "openai", "OpenAI", "openai.com", "openai"},
	{[]string{"claude.ai", "anthropic.com"}, "claude", "Claude", "claude.ai", "claude"},
	{[]string{"gemini.google.com"}, "gemini", "Gemini", "gemini.google.com", "gemini"},
	{[]string{"deepseek.com"}, "deepseek", "DeepSeek", "deepseek.com", "deepseek"},
	{[]string{"grok.com", "x.ai"}, "grok", "Grok", "grok.com", "grok"},
	{[]string{"midjourney.com"}, "midjourney", "Midjourney", "midjourney.com", "midjourney"},
	{[]string{"perplexity.ai"}, "perplexity", "Perplexity", "perplexity.ai", "perplexity"},
	{[]string{"mistral.ai"}, "mistral", "Mistral AI", "mistral.ai", "mistral"},
	{[]string{"huggingface.co"}, "huggingface", "Hugging Face", "huggingface.co", "huggingface"},
	{[]string{"cohere.com"}, "cohere", "Cohere", "cohere.com", "cohere"},
	{[]string{"cursor.com"}, "cursor", "Cursor", "cursor.com", "cursor"},
	{[]string{"meta.ai", "meta.com", "metaaivm.com"}, "meta", "Meta AI", "meta.ai", "meta"},
	{[]string{"copilot.microsoft.com"}, "copilot", "Copilot", "copilot.microsoft.com", "copilot"},
	{[]string{"suno.com"}, "suno", "Suno", "suno.com", "suno"},
	{[]string{"elevenlabs.io"}, "elevenlabs", "ElevenLabs", "elevenlabs.io", "elevenlabs"},
	{[]string{"replicate.com"}, "replicate", "Replicate", "replicate.com", "replicate"},
	{[]string{"character.ai"}, "characterai", "Character.AI", "character.ai", "characterai"},
	{[]string{"runwayml.com"}, "runway", "Runway", "runwayml.com", "runway"},
	{[]string{"stability.ai"}, "stablediffusion", "Stable Diffusion", "stability.ai", "stablediffusion"},
	{[]string{"spotify.com", "scdn.co"}, "spotify", "Spotify", "spotify.com", "spotify"},
	{[]string{"music.apple.com"}, "applemusic", "Apple Music", "music.apple.com", "applemusic"},
	{[]string{"music.youtube.com"}, "youtubemusic", "YouTube Music", "music.youtube.com", "youtubemusic"},
	{[]string{"soundcloud.com"}, "soundcloud", "SoundCloud", "soundcloud.com", "soundcloud"},
	{[]string{"tidal.com"}, "tidal", "Tidal", "tidal.com", "tidal"},
	{[]string{"deezer.com"}, "deezer", "Deezer", "deezer.com", "deezer"},
	{[]string{"pandora.com"}, "pandora", "Pandora", "pandora.com", "pandora"},
	{[]string{"bandcamp.com"}, "bandcamp", "Bandcamp", "bandcamp.com", "bandcamp"},
	{[]string{"last.fm"}, "lastfm", "Last.fm", "last.fm", "lastfm"},
	{[]string{"iheartradio.com"}, "iheartradio", "iHeartRadio", "iheartradio.com", "iheartradio"},
	{[]string{"tunein.com"}, "tunein", "TuneIn", "tunein.com", "tunein"},
	{[]string{"audius.co"}, "audius", "Audius", "audius.co", "audius"},
	{[]string{"shazam.com"}, "shazam", "Shazam", "shazam.com", "shazam"},
	{[]string{"steampowered.com", "steamcommunity.com"}, "steam", "Steam", "steampowered.com", "steam"},
	{[]string{"epicgames.com"}, "epic", "Epic Games", "epicgames.com", "epic"},
	{[]string{"playstation.com"}, "playstation", "PlayStation", "playstation.com", "playstation"},
	{[]string{"xbox.com"}, "xbox", "Xbox", "xbox.com", "xbox"},
	{[]string{"roblox.com"}, "roblox", "Roblox", "roblox.com", "roblox"},
	{[]string{"riotgames.com"}, "riotgames", "Riot Games", "riotgames.com", "riotgames"},
	{[]string{"battle.net"}, "battlenet", "Battle.net", "battle.net", "battlenet"},
	{[]string{"ea.com"}, "ea", "EA", "ea.com", "ea"},
	{[]string{"ubisoft.com"}, "ubisoft", "Ubisoft", "ubisoft.com", "ubisoft"},
	{[]string{"rockstargames.com"}, "rockstargames", "Rockstar Games", "rockstargames.com", "rockstargames"},
	{[]string{"gog.com"}, "gog", "GOG", "gog.com", "gog"},
	{[]string{"itch.io"}, "itch", "itch.io", "itch.io", "itch"},
	{[]string{"humblebundle.com"}, "humble", "Humble Bundle", "humblebundle.com", "humble"},
	{[]string{"origin.com"}, "origin", "Origin", "origin.com", "origin"},
	{[]string{"valvesoftware.com"}, "valve", "Valve", "valvesoftware.com", "valve"},
	{[]string{"hoyoverse.com"}, "hoyoverse", "HoYoverse", "hoyoverse.com", "hoyoverse"},
	{[]string{"supercell.com"}, "supercell", "Supercell", "supercell.com", "supercell"},
	{[]string{"zynga.com"}, "zynga", "Zynga", "zynga.com", "zynga"},
	{[]string{"activision.com"}, "activision", "Activision", "activision.com", "activision"},
	{[]string{"bethesda.net"}, "bethesda", "Bethesda", "bethesda.net", "bethesda"},
	{[]string{"square-enix.com"}, "squareenix", "Square Enix", "square-enix.com", "squareenix"},
	{[]string{"atari.com"}, "atari", "Atari", "atari.com", "atari"},
	{[]string{"nintendo.com"}, "nintendo", "Nintendo", "nintendo.com", "nintendo"},
	{[]string{"minecraft.net"}, "minecraft", "Minecraft", "minecraft.net", "minecraft"},
	{[]string{"google.com", "googleapis.com", "gstatic.com"}, "google", "Google", "google.com", "google"},
	{[]string{"mail.google.com", "gmail.com"}, "gmail", "Gmail", "mail.google.com", "gmail"},
	{[]string{"drive.google.com"}, "googledrive", "Google Drive", "drive.google.com", "googledrive"},
	{[]string{"maps.google.com"}, "googlemaps", "Google Maps", "maps.google.com", "googlemaps"},
	{[]string{"scholar.google.com"}, "googlescholar", "Google Scholar", "scholar.google.com", "googlescholar"},
	{[]string{"notion.so"}, "notion", "Notion", "notion.so", "notion"},
	{[]string{"slack.com"}, "slack", "Slack", "slack.com", "slack"},
	{[]string{"zoom.us"}, "zoom", "Zoom", "zoom.us", "zoom"},
	{[]string{"dropbox.com"}, "dropbox", "Dropbox", "dropbox.com", "dropbox"},
	{[]string{"box.com"}, "box", "Box", "box.com", "box"},
	{[]string{"icloud.com"}, "icloud", "iCloud", "icloud.com", "icloud"},
	{[]string{"onedrive.live.com"}, "onedrive", "OneDrive", "onedrive.live.com", "onedrive"},
	{[]string{"github.com", "githubusercontent.com"}, "github", "GitHub", "github.com", "github"},
	{[]string{"gitlab.com"}, "gitlab", "GitLab", "gitlab.com", "gitlab"},
	{[]string{"stackoverflow.com", "stackexchange.com"}, "stackoverflow", "Stack Overflow", "stackoverflow.com", "stackoverflow"},
	{[]string{"wikipedia.org", "wikimedia.org"}, "wikipedia", "Wikipedia", "wikipedia.org", "wikipedia"},
	{[]string{"figma.com"}, "figma", "Figma", "figma.com", "figma"},
	{[]string{"adobe.com"}, "adobe", "Adobe", "adobe.com", "adobe"},
	{[]string{"microsoft.com", "office.com", "outlook.com", "live.com"}, "microsoft", "Microsoft", "microsoft.com", "microsoft"},
	{[]string{"apple.com"}, "apple", "Apple", "apple.com", "apple"},
	{[]string{"evernote.com"}, "evernote", "Evernote", "evernote.com", "evernote"},
	{[]string{"asana.com"}, "asana", "Asana", "asana.com", "asana"},
	{[]string{"trello.com"}, "trello", "Trello", "trello.com", "trello"},
	{[]string{"monday.com"}, "monday", "Monday.com", "monday.com", "monday"},
	{[]string{"clickup.com"}, "clickup", "ClickUp", "clickup.com", "clickup"},
	{[]string{"airtable.com"}, "airtable", "Airtable", "airtable.com", "airtable"},
	{[]string{"miro.com"}, "miro", "Miro", "miro.com", "miro"},
	{[]string{"todoist.com"}, "todoist", "Todoist", "todoist.com", "todoist"},
	{[]string{"grammarly.com"}, "grammarly", "Grammarly", "grammarly.com", "grammarly"},
	{[]string{"nordvpn.com"}, "nordvpn", "NordVPN", "nordvpn.com", "nordvpn"},
	{[]string{"overleaf.com"}, "overleaf", "Overleaf", "overleaf.com", "overleaf"},
	{[]string{"proton.me", "protonmail.com"}, "proton", "Proton", "proton.me", "proton"},
	{[]string{"tuta.com"}, "tutanota", "Tuta", "tuta.com", "tutanota"},
	{[]string{"mega.nz"}, "mega", "MEGA", "mega.nz", "mega"},
	{[]string{"fastmail.com"}, "fastmail", "Fastmail", "fastmail.com", "fastmail"},
	{[]string{"canva.com"}, "canva", "Canva", "canva.com", "canva"},
	{[]string{"expressvpn.com"}, "expressvpn", "ExpressVPN", "expressvpn.com", "expressvpn"},
	{[]string{"surfshark.com"}, "surfshark", "Surfshark", "surfshark.com", "surfshark"},
	{[]string{"protonvpn.com"}, "protonvpn", "Proton VPN", "protonvpn.com", "protonvpn"},
	{[]string{"mullvad.net"}, "mullvad", "Mullvad", "mullvad.net", "mullvad"},
	{[]string{"amazon.com"}, "amazon", "Amazon", "amazon.com", "amazon"},
	{[]string{"ebay.com"}, "ebay", "eBay", "ebay.com", "ebay"},
	{[]string{"paypal.com"}, "paypal", "PayPal", "paypal.com", "paypal"},
	{[]string{"etsy.com"}, "etsy", "Etsy", "etsy.com", "etsy"},
	{[]string{"shopify.com"}, "shopify", "Shopify", "shopify.com", "shopify"},
	{[]string{"airbnb.com"}, "airbnb", "Airbnb", "airbnb.com", "airbnb"},
	{[]string{"booking.com"}, "booking", "Booking.com", "booking.com", "booking"},
	{[]string{"agoda.com"}, "agoda", "Agoda", "agoda.com", "agoda"},
	{[]string{"expedia.com"}, "expedia", "Expedia", "expedia.com", "expedia"},
	{[]string{"tripadvisor.com"}, "tripadvisor", "Tripadvisor", "tripadvisor.com", "tripadvisor"},
	{[]string{"uber.com"}, "uber", "Uber", "uber.com", "uber"},
	{[]string{"lyft.com"}, "lyft", "Lyft", "lyft.com", "lyft"},
	{[]string{"doordash.com"}, "doordash", "DoorDash", "doordash.com", "doordash"},
	{[]string{"grubhub.com"}, "grubhub", "Grubhub", "grubhub.com", "grubhub"},
	{[]string{"deliveroo.com"}, "deliveroo", "Deliveroo", "deliveroo.com", "deliveroo"},
	{[]string{"walmart.com"}, "walmart", "Walmart", "walmart.com", "walmart"},
	{[]string{"target.com"}, "target", "Target", "target.com", "target"},
	{[]string{"bestbuy.com"}, "bestbuy", "Best Buy", "bestbuy.com", "bestbuy"},
	{[]string{"wish.com"}, "wish", "Wish", "wish.com", "wish"},
	{[]string{"costco.com"}, "costco", "Costco", "costco.com", "costco"},
	{[]string{"ikea.com"}, "ikea", "IKEA", "ikea.com", "ikea"},
	{[]string{"nike.com"}, "nike", "Nike", "nike.com", "nike"},
	{[]string{"adidas.com"}, "adidas", "Adidas", "adidas.com", "adidas"},
	{[]string{"sephora.com"}, "sephora", "Sephora", "sephora.com", "sephora"},
	{[]string{"zalando.com"}, "zalando", "Zalando", "zalando.com", "zalando"},
	{[]string{"amazonaws.com"}, "aws", "AWS", "amazonaws.com", "aws"},
	{[]string{"cnn.com"}, "cnn", "CNN", "cnn.com", "cnn"},
	{[]string{"nytimes.com"}, "nytimes", "The New York Times", "nytimes.com", "nytimes"},
	{[]string{"theguardian.com"}, "theguardian", "The Guardian", "theguardian.com", "theguardian"},
	{[]string{"reuters.com"}, "reuters", "Reuters", "reuters.com", "reuters"},
	{[]string{"forbes.com"}, "forbes", "Forbes", "forbes.com", "forbes"},
	{[]string{"washingtonpost.com"}, "washingtonpost", "The Washington Post", "washingtonpost.com", "washingtonpost"},
	{[]string{"wsj.com"}, "wsj", "The Wall Street Journal", "wsj.com", "wsj"},
	{[]string{"economist.com"}, "economist", "The Economist", "economist.com", "economist"},
	{[]string{"time.com"}, "time", "TIME", "time.com", "time"},
	{[]string{"aljazeera.com"}, "aljazeera", "Al Jazeera", "aljazeera.com", "aljazeera"},
	{[]string{"dw.com"}, "dw", "DW", "dw.com", "dw"},
	{[]string{"france24.com"}, "france24", "France 24", "france24.com", "france24"},
	{[]string{"axios.com"}, "axios", "Axios", "axios.com", "axios"},
	{[]string{"politico.com"}, "politico", "Politico", "politico.com", "politico"},
	{[]string{"huffpost.com"}, "huffpost", "HuffPost", "huffpost.com", "huffpost"},
	{[]string{"buzzfeed.com"}, "buzzfeed", "BuzzFeed", "buzzfeed.com", "buzzfeed"},
	{[]string{"vice.com"}, "vice", "Vice", "vice.com", "vice"},
	{[]string{"wired.com"}, "wired", "Wired", "wired.com", "wired"},
	{[]string{"theverge.com"}, "verge", "The Verge", "theverge.com", "verge"},
	{[]string{"techcrunch.com"}, "techcrunch", "TechCrunch", "techcrunch.com", "techcrunch"},
	{[]string{"engadget.com"}, "engadget", "Engadget", "engadget.com", "engadget"},
	{[]string{"gizmodo.com"}, "gizmodo", "Gizmodo", "gizmodo.com", "gizmodo"},
	{[]string{"cnet.com"}, "cnet", "CNET", "cnet.com", "cnet"},
	{[]string{"bbc.com", "bbc.co.uk"}, "bbc", "BBC", "bbc.com", "bbc"},
	{[]string{"bloomberg.com"}, "bloomberg", "Bloomberg", "bloomberg.com", "bloomberg"},
	{[]string{"codepen.io"}, "codepen", "CodePen", "codepen.io", "codepen"},
	{[]string{"replit.com"}, "replit", "Replit", "replit.com", "replit"},
	{[]string{"vercel.com"}, "vercel", "Vercel", "vercel.com", "vercel"},
	{[]string{"netlify.com"}, "netlify", "Netlify", "netlify.com", "netlify"},
	{[]string{"heroku.com"}, "heroku", "Heroku", "heroku.com", "heroku"},
	{[]string{"digitalocean.com"}, "digitalocean", "DigitalOcean", "digitalocean.com", "digitalocean"},
	{[]string{"linode.com"}, "linode", "Linode", "linode.com", "linode"},
	{[]string{"vultr.com"}, "vultr", "Vultr", "vultr.com", "vultr"},
	{[]string{"cloudflare.com"}, "cloudflare", "Cloudflare", "cloudflare.com", "cloudflare"},
	{[]string{"npmjs.com"}, "npm", "npm", "npmjs.com", "npm"},
	{[]string{"docker.com"}, "docker", "Docker", "docker.com", "docker"},
	{[]string{"kubernetes.io"}, "kubernetes", "Kubernetes", "kubernetes.io", "kubernetes"},
	{[]string{"raspberrypi.com"}, "raspberrypi", "Raspberry Pi", "raspberrypi.com", "raspberrypi"},
	{[]string{"arduino.cc"}, "arduino", "Arduino", "arduino.cc", "arduino"},
	{[]string{"news.ycombinator.com"}, "ycombinator", "Hacker News", "news.ycombinator.com", "ycombinator"},
	{[]string{"producthunt.com"}, "producthunt", "Product Hunt", "producthunt.com", "producthunt"},
	{[]string{"dev.to"}, "devdotto", "DEV", "dev.to", "devdotto"},
	{[]string{"hashnode.com"}, "hashnode", "Hashnode", "hashnode.com", "hashnode"},
	{[]string{"jsfiddle.net"}, "jsfiddle", "JSFiddle", "jsfiddle.net", "jsfiddle"},
	{[]string{"binance.com"}, "binance", "Binance", "binance.com", "binance"},
	{[]string{"coinbase.com"}, "coinbase", "Coinbase", "coinbase.com", "coinbase"},
	{[]string{"kraken.com"}, "kraken", "Kraken", "kraken.com", "kraken"},
	{[]string{"okx.com"}, "okx", "OKX", "okx.com", "okx"},
	{[]string{"bybit.com"}, "bybit", "Bybit", "bybit.com", "bybit"},
	{[]string{"bitstamp.net"}, "bitstamp", "Bitstamp", "bitstamp.net", "bitstamp"},
	{[]string{"metamask.io"}, "metamask", "MetaMask", "metamask.io", "metamask"},
	{[]string{"trustwallet.com"}, "trustwallet", "Trust Wallet", "trustwallet.com", "trustwallet"},
	{[]string{"ledger.com"}, "ledger", "Ledger", "ledger.com", "ledger"},
	{[]string{"trezor.io"}, "trezor", "Trezor", "trezor.io", "trezor"},
	{[]string{"uniswap.org"}, "uniswap", "Uniswap", "uniswap.org", "uniswap"},
	{[]string{"coingecko.com"}, "coingecko", "CoinGecko", "coingecko.com", "coingecko"},
	{[]string{"coinmarketcap.com"}, "coinmarketcap", "CoinMarketCap", "coinmarketcap.com", "coinmarketcap"},
	{[]string{"tradingview.com"}, "tradingview", "TradingView", "tradingview.com", "tradingview"},
	{[]string{"wise.com"}, "wise", "Wise", "wise.com", "wise"},
	{[]string{"revolut.com"}, "revolut", "Revolut", "revolut.com", "revolut"},
	{[]string{"stripe.com"}, "stripe", "Stripe", "stripe.com", "stripe"},
	{[]string{"squareup.com"}, "square", "Square", "squareup.com", "square"},
	{[]string{"cash.app"}, "cashapp", "Cash App", "cash.app", "cashapp"},
	{[]string{"venmo.com"}, "venmo", "Venmo", "venmo.com", "venmo"},
	{[]string{"coursera.org"}, "coursera", "Coursera", "coursera.org", "coursera"},
	{[]string{"udemy.com"}, "udemy", "Udemy", "udemy.com", "udemy"},
	{[]string{"duolingo.com"}, "duolingo", "Duolingo", "duolingo.com", "duolingo"},
	{[]string{"khanacademy.org"}, "khanacademy", "Khan Academy", "khanacademy.org", "khanacademy"},
	{[]string{"edx.org"}, "edx", "edX", "edx.org", "edx"},
	{[]string{"skillshare.com"}, "skillshare", "Skillshare", "skillshare.com", "skillshare"},
	{[]string{"masterclass.com"}, "masterclass", "MasterClass", "masterclass.com", "masterclass"},
	{[]string{"ted.com"}, "ted", "TED", "ted.com", "ted"},
	{[]string{"imdb.com"}, "imdb", "IMDb", "imdb.com", "imdb"},
	{[]string{"medium.com"}, "medium", "Medium", "medium.com", "medium"},
	{[]string{"quora.com"}, "quora", "Quora", "quora.com", "quora"},
	{[]string{"fandom.com"}, "fandom", "Fandom", "fandom.com", "fandom"},
	{[]string{"dribbble.com"}, "dribbble", "Dribbble", "dribbble.com", "dribbble"},
	{[]string{"behance.net"}, "behance", "Behance", "behance.net", "behance"},
	{[]string{"speedtest.net"}, "speedtest", "Speedtest", "speedtest.net", "speedtest"},
	{[]string{"letterboxd.com"}, "letterboxd", "Letterboxd", "letterboxd.com", "letterboxd"},
	{[]string{"trakt.tv"}, "trakt", "Trakt", "trakt.tv", "trakt"},
	{[]string{"goodreads.com"}, "goodreads", "Goodreads", "goodreads.com", "goodreads"},
	{[]string{"wattpad.com"}, "wattpad", "Wattpad", "wattpad.com", "wattpad"},
	{[]string{"archive.org"}, "internetarchive", "Internet Archive", "archive.org", "internetarchive"},
	{[]string{"openstreetmap.org"}, "openstreetmap", "OpenStreetMap", "openstreetmap.org", "openstreetmap"},
	{[]string{"meetup.com"}, "meetup", "Meetup", "meetup.com", "meetup"},
	{[]string{"eventbrite.com"}, "eventbrite", "Eventbrite", "eventbrite.com", "eventbrite"},
	{[]string{"yelp.com"}, "yelp", "Yelp", "yelp.com", "yelp"},
	{[]string{"zillow.com"}, "zillow", "Zillow", "zillow.com", "zillow"},
	{[]string{"indeed.com"}, "indeed", "Indeed", "indeed.com", "indeed"},
	{[]string{"glassdoor.com"}, "glassdoor", "Glassdoor", "glassdoor.com", "glassdoor"},
	{[]string{"fiverr.com"}, "fiverr", "Fiverr", "fiverr.com", "fiverr"},
	{[]string{"upwork.com"}, "upwork", "Upwork", "upwork.com", "upwork"},
	{[]string{"deepl.com"}, "deepl", "DeepL", "deepl.com", "deepl"},
}

// siteSuffixIndex 是按后缀长度从长到短排好的匹配表：最长后缀优先，
// tv.apple.com 归 Apple TV+ 而不是 Apple，drive.google.com 归 Google。
type siteSuffixEntry struct {
	suffix  string
	service *siteService
}

var siteSuffixIndex []siteSuffixEntry

func init() {
	for i := range siteServices {
		for _, suffix := range siteServices[i].suffixes {
			siteSuffixIndex = append(siteSuffixIndex, siteSuffixEntry{suffix: suffix, service: &siteServices[i]})
		}
	}
	sort.Slice(siteSuffixIndex, func(a, b int) bool {
		return len(siteSuffixIndex[a].suffix) > len(siteSuffixIndex[b].suffix)
	})
}

// mapSiteHost 把连接域名归到服务：返回服务 id；未映射的域名返回以域名为
// 键的键，空域名返回未知服务键。
func mapSiteHost(host string) (key string, service *siteService) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "unknown", nil
	}
	for _, entry := range siteSuffixIndex {
		if host == entry.suffix || strings.HasSuffix(host, "."+entry.suffix) {
			return entry.service.id, entry.service
		}
	}
	return "domain:" + host, nil
}

func siteServiceByID(id string) *siteService {
	for i := range siteServices {
		if siteServices[i].id == id {
			return &siteServices[i]
		}
	}
	return nil
}

const (
	siteLedgerRetentionDays = 31
	siteRecordsMaxBytes     = 16 << 20
	siteAggregateMinGap     = 5 * time.Second
	siteCleanupMinGap       = time.Hour
)

var (
	siteAggregateMu   sync.Mutex
	siteLastAggregate time.Time
	siteLastCleanup   time.Time
)

func siteLedgerDir(dataDir string) string {
	return filepath.Join(dataDir, "site-ledger")
}

func siteLedgerOffsetPath(dataDir string) string {
	return filepath.Join(siteLedgerDir(dataDir), "offset")
}

func siteLedgerDayPath(dataDir, date string) string {
	return filepath.Join(siteLedgerDir(dataDir), date+".json")
}

func writeFileAtomic(path string, body []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readSiteOffset(dataDir string) (int64, error) {
	raw, err := os.ReadFile(siteLedgerOffsetPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || offset < 0 {
		// 偏移文件损坏：跳到文件末尾，只丢聚合间隙的新记录，
		// 绝不从头重算导致整月翻倍。
		return -1, nil
	}
	return offset, nil
}

// aggregateSiteRecords 把核心记录文件里的新记录聚合进日账本。force 为
// false 时 5 秒内只跑一次（仪表盘 2 秒一刷时不重复写盘）；钻取接口用
// force 保证看到最新数据。失败只记日志，不影响仪表盘和钻取返回。
func aggregateSiteRecords(dataDir, relayUsername string, force bool) {
	siteAggregateMu.Lock()
	defer siteAggregateMu.Unlock()
	now := time.Now()
	if !force && now.Sub(siteLastAggregate) < siteAggregateMinGap {
		return
	}
	siteLastAggregate = now
	if err := aggregateSiteRecordsOnce(dataDir, relayUsername, now); err != nil {
		log.Printf("聚合网站排行账本失败：%v", err)
	}
}

func aggregateSiteRecordsOnce(dataDir, relayUsername string, now time.Time) error {
	maybeCleanupSiteLedgers(dataDir, now)
	recordsPath := proxy.SiteRecordsPath(dataDir)
	info, err := os.Stat(recordsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	size := info.Size()
	offset, err := readSiteOffset(dataDir)
	if err != nil {
		return err
	}
	if offset < 0 {
		offset = size
	}
	if offset > size {
		// 记录文件被截断或轮转过，从头开始。
		offset = 0
	}
	if offset == size {
		return nil
	}
	raw, err := readSiteRecordsRange(recordsPath, offset, size)
	if err != nil {
		return err
	}
	// 只处理以换行结尾的完整行：最后一行可能正在被核心写入，
	// 留到下次再读，不丢也不重。
	lastNewline := bytes.LastIndexByte(raw, '\n')
	if lastNewline < 0 {
		return nil
	}
	complete := raw[:lastNewline+1]
	newOffset := offset + int64(len(complete))
	byDay := map[string][]siteRecord{}
	scanner := bufio.NewScanner(bytes.NewReader(complete))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record siteRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.User == "" || record.Upload+record.Download == 0 {
			continue
		}
		day := time.Unix(record.End, 0).Format("2006-01-02")
		byDay[day] = append(byDay[day], record)
	}
	for day, records := range byDay {
		ledger, err := loadDaySiteLedger(dataDir, day)
		if err != nil {
			return err
		}
		for _, record := range records {
			key, _ := mapSiteHost(record.Host)
			userServices := ledger.Services[record.User]
			if userServices == nil {
				userServices = map[string]*siteTotals{}
				ledger.Services[record.User] = userServices
			}
			totals := userServices[key]
			if totals == nil {
				totals = &siteTotals{}
				userServices[key] = totals
			}
			totals.Upload += record.Upload
			totals.Download += record.Download
			duration := record.End - record.Start
			if duration > 0 {
				totals.Duration += duration
			}
		}
		body, err := json.Marshal(ledger)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(siteLedgerDayPath(dataDir, day), body, 0600); err != nil {
			return err
		}
	}
	if err := writeFileAtomic(siteLedgerOffsetPath(dataDir), []byte(strconv.FormatInt(newOffset, 10)), 0600); err != nil {
		return err
	}
	// 记录文件已全部消费且过大时截断归零：核心以 O_APPEND 打开，
	// 截断后新记录仍从文件头写，不会丢到旧偏移里。
	if size > siteRecordsMaxBytes && newOffset == size {
		if err := os.Truncate(recordsPath, 0); err != nil {
			return err
		}
		if err := writeFileAtomic(siteLedgerOffsetPath(dataDir), []byte("0"), 0600); err != nil {
			return err
		}
	}
	return nil
}

func readSiteRecordsRange(path string, offset, size int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, size-offset))
}

func loadDaySiteLedger(dataDir, date string) (*daySiteLedger, error) {
	ledger := newDaySiteLedger(date)
	raw, err := os.ReadFile(siteLedgerDayPath(dataDir, date))
	if err != nil {
		if os.IsNotExist(err) {
			return ledger, nil
		}
		return nil, err
	}
	var decoded daySiteLedger
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// 账本文件损坏：从空账本重算当天的增量，旧明细丢弃。
		log.Printf("网站排行账本 %s 损坏，从空账本重算", date)
		return ledger, nil
	}
	if decoded.Services == nil {
		decoded.Services = map[string]map[string]*siteTotals{}
	}
	decoded.Date = date
	return &decoded, nil
}

var siteLedgerDayFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.json$`)

func maybeCleanupSiteLedgers(dataDir string, now time.Time) {
	if now.Sub(siteLastCleanup) < siteCleanupMinGap {
		return
	}
	siteLastCleanup = now
	entries, err := os.ReadDir(siteLedgerDir(dataDir))
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -(siteLedgerRetentionDays - 1)).Format("2006-01-02")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parts := siteLedgerDayFilePattern.FindStringSubmatch(entry.Name())
		if parts == nil || parts[1] >= cutoff {
			continue
		}
		_ = os.Remove(filepath.Join(siteLedgerDir(dataDir), entry.Name()))
	}
}

// siteRow 是钻取接口返回的一行：流量为主，时长为辅。
type siteRow struct {
	Name     string `json:"name"`
	Domain   string `json:"domain"`
	Icon     string `json:"icon"`
	Bytes    int64  `json:"bytes"`
	Duration int64  `json:"duration"`
}

type siteRanking struct {
	Sites      []siteRow `json:"sites"`
	OtherCount int       `json:"otherCount"`
	OtherBytes int64     `json:"otherBytes"`
}

// buildSiteRanking 把某用户某天的账本排成前 10 + 其余合计。
func buildSiteRanking(ledger *daySiteLedger, username string) siteRanking {
	ranking := siteRanking{Sites: []siteRow{}}
	if ledger == nil || username == "" {
		return ranking
	}
	userServices := ledger.Services[username]
	if len(userServices) == 0 {
		return ranking
	}
	type scored struct {
		key    string
		totals *siteTotals
	}
	rows := make([]scored, 0, len(userServices))
	for key, totals := range userServices {
		if totals == nil || totals.bytes() == 0 {
			continue
		}
		rows = append(rows, scored{key: key, totals: totals})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].totals.bytes() != rows[b].totals.bytes() {
			return rows[a].totals.bytes() > rows[b].totals.bytes()
		}
		return rows[a].key < rows[b].key
	})
	for i, row := range rows {
		if i >= 10 {
			ranking.OtherCount++
			ranking.OtherBytes += row.totals.bytes()
			continue
		}
		ranking.Sites = append(ranking.Sites, siteRowFor(row.key, row.totals))
	}
	return ranking
}

func siteRowFor(key string, totals *siteTotals) siteRow {
	if key == "unknown" {
		return siteRow{Name: "未知域名", Domain: "", Icon: "un", Bytes: totals.bytes(), Duration: totals.Duration}
	}
	if service := siteServiceByID(key); service != nil {
		return siteRow{Name: service.name, Domain: service.domain, Icon: service.icon, Bytes: totals.bytes(), Duration: totals.Duration}
	}
	if host, ok := strings.CutPrefix(key, "domain:"); ok {
		return siteRow{Name: host, Domain: host, Icon: "un", Bytes: totals.bytes(), Duration: totals.Duration}
	}
	return siteRow{Name: key, Domain: key, Icon: "un", Bytes: totals.bytes(), Duration: totals.Duration}
}

// liveSiteIcons 查询核心 clash_api 的活动连接，去重后返回图标键：
// 只看图标，不分用户，正好绕开 clash_api 没有用户字段的坎。
// 核心没起、没配 clash_api、查询失败都返回空，不影响仪表盘。
func liveSiteIcons(dataDir string, statsPort int) []string {
	icons := []string{}
	secret := proxy.ReadClashSecret(dataDir)
	if secret == "" {
		return icons
	}
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/connections", proxy.ClashPort(statsPort)), nil)
	if err != nil {
		return icons
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := client.Do(request)
	if err != nil {
		return icons
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return icons
	}
	var snapshot struct {
		Connections []struct {
			Metadata struct {
				Host string `json:"host"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		return icons
	}
	seen := map[string]bool{}
	for _, connection := range snapshot.Connections {
		host := strings.TrimSpace(connection.Metadata.Host)
		if host == "" {
			continue
		}
		_, service := mapSiteHost(host)
		icon := "un"
		if service != nil {
			icon = service.icon
		}
		// 预览只看图标：同一图标出现一次就够了。
		if seen[icon] {
			continue
		}
		seen[icon] = true
		icons = append(icons, icon)
		if len(icons) >= 30 {
			break
		}
	}
	return icons
}
