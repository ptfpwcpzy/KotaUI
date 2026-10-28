package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
	"golang.org/x/net/publicsuffix"
)

// 网站排行账本：核心在连接关闭时写一条 JSON 记录，面板增量聚合到每天一个
// 文件、只留 31 天。总量不单独计数，由明细求和得出；现有 v2ray 总量统计
// 不动，两套账各司其职。

// siteRecord 是核心 site_log 写出的一行。
type siteRecord struct {
	User     string `json:"user"`
	Host     string `json:"host"`
	Dst      string `json:"dst"`
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
	{[]string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtube-nocookie.com", "ggpht.cn", "withyoutube.com", "youtube.ae", "youtube.al", "youtube.am", "youtube.at", "youtube.az", "youtube.ba", "youtube.be", "youtube.bg", "youtube.bh", "youtube.bo", "youtube.by", "youtube.ca", "youtube.cat", "youtube.ch", "youtube.cl", "youtube.co", "youtube.co.ae", "youtube.co.at", "youtube.co.cr", "youtube.co.hu", "youtube.co.id", "youtube.co.il", "youtube.co.in", "youtube.co.jp", "youtube.co.ke", "youtube.co.kr", "youtube.co.ma", "youtube.co.nz", "youtube.co.th", "youtube.co.tz", "youtube.co.ug", "youtube.co.uk", "youtube.co.ve", "youtube.co.za", "youtube.co.zw", "youtube.com.ar", "youtube.com.au", "youtube.com.az", "youtube.com.bd", "youtube.com.bh", "youtube.com.bo", "youtube.com.br", "youtube.com.by", "youtube.com.co", "youtube.com.do", "youtube.com.ec", "youtube.com.ee", "youtube.com.eg", "youtube.com.es", "youtube.com.gh", "youtube.com.gr", "youtube.com.gt", "youtube.com.hk", "youtube.com.hn", "youtube.com.hr", "youtube.com.jm", "youtube.com.jo", "youtube.com.kw", "youtube.com.lb", "youtube.com.lv", "youtube.com.ly", "youtube.com.mk", "youtube.com.mt", "youtube.com.mx", "youtube.com.my", "youtube.com.ng", "youtube.com.ni", "youtube.com.om", "youtube.com.pa", "youtube.com.pe", "youtube.com.ph", "youtube.com.pk", "youtube.com.pt", "youtube.com.py", "youtube.com.qa", "youtube.com.ro", "youtube.com.sa", "youtube.com.sg", "youtube.com.sv", "youtube.com.tn", "youtube.com.tr", "youtube.com.tw", "youtube.com.ua", "youtube.com.uy", "youtube.com.ve", "youtube.cr", "youtube.cz", "youtube.de", "youtube.dk", "youtube.ee", "youtube.es", "youtube.fi", "youtube.fr", "youtube.ge", "youtube.gr", "youtube.gt", "youtube.hk", "youtube.hr", "youtube.hu", "youtube.ie", "youtube.in", "youtube.iq", "youtube.is", "youtube.it", "youtube.jo", "youtube.jp", "youtube.kr", "youtube.kz", "youtube.la", "youtube.lk", "youtube.lt", "youtube.lu", "youtube.lv", "youtube.ly", "youtube.ma", "youtube.md", "youtube.me", "youtube.mk", "youtube.mn", "youtube.mx", "youtube.my", "youtube.ng", "youtube.ni", "youtube.nl", "youtube.no", "youtube.pa", "youtube.pe", "youtube.ph", "youtube.pk", "youtube.pl", "youtube.pr", "youtube.pt", "youtube.qa", "youtube.ro", "youtube.rs", "youtube.ru", "youtube.sa", "youtube.se", "youtube.sg", "youtube.si", "youtube.sk", "youtube.sn", "youtube.soy", "youtube.sv", "youtube.tn", "youtube.tv", "youtube.ua", "youtube.ug", "youtube.uy", "youtube.vn", "youtubeeducation.com", "youtubefanfest.com", "youtubegaming.com", "youtubego.co.id", "youtubego.co.in", "youtubego.com", "youtubego.com.br", "youtubego.id", "youtubego.in", "youtubekids.com", "youtubemobilesupport.com", "yt.be"}, "youtube", "YouTube", "youtube.com", "youtube"},
	{[]string{"netflix.com", "nflxext.com", "nflximg.net", "nflxvideo.net", "nflxso.net", "netflix.net", "fast.com", "netflix.ca", "netflixdnstest0.com", "netflixdnstest1.com", "netflixdnstest10.com", "netflixdnstest2.com", "netflixdnstest3.com", "netflixdnstest4.com", "netflixdnstest5.com", "netflixdnstest6.com", "netflixdnstest7.com", "netflixdnstest8.com", "netflixdnstest9.com", "netflixinvestor.com", "netflixtechblog.com", "nflximg.com", "nflxsearch.net"}, "netflix", "Netflix", "netflix.com", "netflix"},
	{[]string{"twitch.tv", "twitchcdn.net", "jtvnw.net", "ttvnw.net", "twitch.com", "ext-twitch.tv", "live-video.net", "twitchcon.com", "twitchsvc.net"}, "twitch", "Twitch", "twitch.tv", "twitch"},
	{[]string{"tiktok.com", "tiktokcdn.com", "tiktokv.com", "ibyteimg.com", "byteoversea.com", "byteoversea.net", "ibytedtos.com", "muscdn.com", "musical.ly", "tiktokcdn-in.com", "tiktokshop.com", "bytedapm.com", "roovza-launches.appsflyersdk.com", "roovza.inapps.appsflyersdk.com", "roovza.launches.appsflyersdk.com", "roovza.skadsdk.appsflyersdk.com", "tik-tokapi.com", "tiktok-minis.com", "tiktok-row.net", "tiktokcdn-eu.com", "tiktokcdn-us.com", "tiktokd.net", "tiktokd.org", "tiktokeu-cdn.com", "tiktokglobalshopv.com", "tiktokminis.us", "tiktokrow-cdn.com", "tiktokv.eu", "tiktokv.us", "tiktokw.eu", "tiktokw.us", "ttcdn-us.com", "ttlivecdn.com", "ttoverseaus.net", "ttwstatic.com"}, "tiktok", "TikTok", "tiktok.com", "tiktok"},
	{[]string{"vimeo.com", "vimeocdn.com", "livestream.com", "vhx.tv", "vhxqa1.com", "vhxqa2.com", "vhxqa3.com", "vhxqa4.com", "vhxqa6.com", "vimeo-staging.com", "vimeo-staging2.com", "vimeo.fr", "vimeobusiness.com", "vimeogoods.com", "vimeoondemand.com", "vimeostatus.com"}, "vimeo", "Vimeo", "vimeo.com", "vimeo"},
	{[]string{"dailymotion.com", "dmcdn.net", "dm-event.net"}, "dailymotion", "Dailymotion", "dailymotion.com", "dailymotion"},
	{[]string{"crunchyroll.com"}, "crunchyroll", "Crunchyroll", "crunchyroll.com", "crunchyroll"},
	{[]string{"max.com", "hbomax.com", "hbo.com", "beforeigners.com", "brightline.tv", "cinemax.com", "discomax.com", "elpionero.es", "forthethrone.com", "h264.io", "hbo-europe.com", "hbo.ba", "hbo.bg", "hbo.com.c.footprint.net", "hbo.cz", "hbo.eu", "hbo.hr", "hbo.hu", "hbo.me", "hbo.mk", "hbo.pl", "hbo.ro", "hbo.rs", "hbo.sk", "hboaccess.com", "hboarchives.com", "hboasia.com", "hboenterprises.com", "hboespana.com", "hbogo.co.th", "hbogo.com", "hbogo.cz", "hbogo.eu", "hbogo.hu", "hbogo.sk", "hbogoasia.com", "hbogoasia.hk", "hbogoasia.id", "hbogoasia.my", "hbogoasia.ph", "hbogoasia.sg", "hbogoasia.tw", "hboinflight.com", "hbolacontent.net", "hbolaunchpad.com", "hbomailman.com", "hbomax-images.warnermediacdn.com", "hbomax.eu", "hbomaxcdn.com", "hbomaxdash.s.llnwi.net", "hbomediarelations.com", "hbonordic.com", "hbonordic.tv", "hbonow.com", "hboportugal.com", "hboprod.com", "hbospain.com", "hbotvsales.com", "homebox.com", "homeboxoffice.com", "maxgo.com", "redbyhbo.com"}, "hbomax", "HBO Max", "max.com", "hbomax"},
	{[]string{"paramountplus.com"}, "paramountplus", "Paramount+", "paramountplus.com", "paramountplus"},
	{[]string{"tubitv.com", "tubi.io", "tubi.tv", "tubi.video"}, "tubi", "Tubi", "tubitv.com", "tubi"},
	{[]string{"plex.tv", "plex.com", "plex.services", "plexapp.com"}, "plex", "Plex", "plex.tv", "plex"},
	{[]string{"kodi.tv", "kodi.wiki"}, "kodi", "Kodi", "kodi.tv", "kodi"},
	{[]string{"viki.com"}, "viki", "Viki", "viki.com", "viki"},
	{[]string{"rumble.com", "rmbl.ws"}, "rumble", "Rumble", "rumble.com", "rumble"},
	{[]string{"odysee.com"}, "odysee", "Odysee", "odysee.com", "odysee"},
	{[]string{"kick.com"}, "kick", "Kick", "kick.com", "kick"},
	{[]string{"vevo.com"}, "vevo", "Vevo", "vevo.com", "vevo"},
	{[]string{"disneyplus.com", "disney-plus.net", "disney.com", "20thcenturystudios.com", "20thcenturystudios.com.au", "20thcenturystudios.com.br", "20thcenturystudios.jp", "abc-studios.com", "abc.com", "abcnews.com", "adventuresbydisney.com", "babble.com", "babyzone.com", "bamgrid.com", "bamtech.net", "beautyandthebeastmusical.co.uk", "detpress.com", "dilcdn.com", "disney-asia.com", "disney-discount.com", "disney-portal.my.onetrust.com", "disney-studio.com", "disney-studio.net", "disney.asia", "disney.be", "disney.bg", "disney.ca", "disney.ch", "disney.co.il", "disney.co.jp", "disney.co.kr", "disney.co.th", "disney.co.uk", "disney.co.za", "disney.com.au", "disney.com.br", "disney.com.hk", "disney.com.tw", "disney.cz", "disney.de", "disney.dk", "disney.es", "disney.fi", "disney.fr", "disney.gr", "disney.hu", "disney.id", "disney.in", "disney.io", "disney.it", "disney.my", "disney.my.sentry.io", "disney.nl", "disney.no", "disney.ph", "disney.pl", "disney.pt", "disney.ro", "disney.ru", "disney.se", "disney.sg", "disneyaccount.com", "disneyadsales.com", "disneyanimation.com", "disneyarena.com", "disneyaulani.com", "disneybaby.com", "disneycareers.com", "disneychannelonstage.com", "disneychannelroadtrip.com", "disneycruisebrasil.com", "disneyenconcert.com", "disneyiejobs.com", "disneyinflight.com", "disneyinternational.com", "disneyinternationalhd.com", "disneyjunior.com", "disneyjuniortreataday.com", "disneylandparis.com", "disneylatino.com", "disneymagicmoments.co.il", "disneymagicmoments.co.uk", "disneymagicmoments.co.za", "disneymagicmoments.de", "disneymagicmoments.es", "disneymagicmoments.fr", "disneymagicmoments.gen.tr", "disneymagicmoments.gr", "disneymagicmoments.it", "disneymagicmoments.pl", "disneymagicmomentsme.com", "disneyme.com", "disneymeetingsandevents.com", "disneymovieinsiders.com", "disneymusicpromotion.com", "disneynewseries.com", "disneynow.com", "disneypeoplesurveys.com", "disneyplus.bn5x.net", "disneyplus.com.ssl.sc.omtrdc.net", "disneyprivacycenter.com", "disneyredirects.com", "disneysrivieraresort.com", "disneystore.com", "disneystreaming.com", "disneysubscription.com", "disneytermsofuse.com", "disneytickets.co.uk", "disneyturkiye.com.tr", "disneytvajobs.com", "disneyworld-go.com", "dmed.technology", "dssott.com", "dtci.co", "dtci.technology", "edgedatg.com", "espn.api.edge.bamgrid.com", "espn.co.uk", "espn.com", "espn.hb.omtrdc.net", "espn.net", "espncdn.com", "espndotcom.tt.omtrdc.net", "espnqa.com", "freeform.com", "fxnetwork.com", "fxnetworks.com", "go-disneyworldgo.com", "go.com", "hongkongdisneyland.com", "hotstar-cdn.net", "hotstar-labs.com", "hotstar.com", "hotstarext.com", "lucasfilm.com", "marvel.com", "marvel10thanniversary.com", "marveldimensionofheroes.com", "marvelparty.net", "marvelpinball.com", "marvelsdoubleagent.com", "marvelspotlightplays.com", "marvelsuperheroseptember.com", "marvelsuperwar.com", "mickey.tv", "moviesanywhere.com", "natgeomaps.com", "nationalgeographic.com", "nationalgeographicpartners.com", "ngeo.com", "nomadlandmovie.ch", "pixar.com", "playmation.com", "searchlightpictures.com", "shanghaidisneyresort.com", "shopdisney.com", "shops-disney.com", "sorcerersarena.com", "spaindisney.com", "star-brasil.com", "star-latam.com", "star.api.edge.bamgrid.com", "star.connections.edge.bamgrid.com", "star.content.edge.bamgrid.com", "star.playback.edge.bamgrid.com", "starott.com", "starplus.com", "starwars.com", "starwarsgalacticstarcruiser.com", "starwarskids.com", "streamingdisney.net", "themarvelexperiencetour.com", "thestationbymaker.com", "thewaltdisneycompany.com", "thisispolaris.com", "tokyodisneyresort.jp", "waltdisneystudios.com", "watchdisneyfe.com", "watchespn.com"}, "disney", "Disney+", "disneyplus.com", "disney"},
	{[]string{"hulu.com", "huluim.com", "112263.com", "callhulu.com", "findyourlimits.com", "freehulu.com", "hooloo.tv", "hoolu.com", "hoolu.tv", "hu1u.com", "huloo.cc", "huloo.tv", "hulu.jp", "hulu.playback.edge.bamgrid.com", "hulu.tv", "hulu.us", "huluaction.com", "huluad.com", "huluapp.com", "huluasks.com", "hulucall.com", "hulufree.com", "hulugans.com", "hulugermany.com", "hulugo.com", "huluinstantmessenger.com", "huluitaly.com", "hulunet.com", "hulunetwork.com", "huluplus.com", "hulupremium.com", "hulupurchase.com", "huluqa.com", "hulurussia.com", "huluspain.com", "hulusports.com", "hulustream.com", "huluteam.com", "hulutv.com", "huluusa.com", "joinmaidez.com", "mushymush.tv", "myhulu.com", "originalhulu.com", "payhulu.com", "registerhulu.com", "thehulubraintrust.com", "wwwhuluplus.com"}, "hulu", "Hulu", "hulu.com", "hulu"},
	{[]string{"primevideo.com", "amazonvideo.com", "aiv-cdn.net", "amazonprimevideo.cn", "amazonprimevideo.com.cn", "amazonprimevideos.com", "amazonvideo.cc", "prime-video.com", "primevideo.cc", "primevideo.info", "primevideo.org", "primevideo.tv", "pv-cdn.net"}, "prime", "Prime Video", "primevideo.com", "prime"},
	{[]string{"peacocktv.com"}, "peacock", "Peacock", "peacocktv.com", "peacock"},
	{[]string{"pluto.tv", "plutotv.net"}, "plutotv", "Pluto TV", "pluto.tv", "plutotv"},
	{[]string{"pornhub.com", "phncdn.com", "phprcdn.com", "pornhubpremium.com", "porngub.com", "pornhub-deutsch.net", "pornhub.org", "pornhubapparel.com"}, "pornhub", "Pornhub", "pornhub.com", "pornhub"},
	{[]string{"xvideos.com", "xv-ru.com", "xvideos-ar.com", "xvideos-cdn.com", "xvideos-india.com"}, "xvideos", "XVideos", "xvideos.com", "xvideos"},
	{[]string{"xnxx.com", "xnxx-cdn.com", "xnxx.net", "xnxx.tv", "xnxx3.com"}, "xnxx", "XNXX", "xnxx.com", "xnxx"},
	{[]string{"redtube.com", "livehdcams.com", "rdtcdn.com", "redtube.com.br", "redtubepremium.com"}, "redtube", "RedTube", "redtube.com", "redtube"},
	{[]string{"youporn.com", "extremetube.com", "keezmovies.com", "yopornshop.com", "youporngay.com", "youpornpremium.com", "youpornru.com", "ypncdn.com"}, "youporn", "YouPorn", "youporn.com", "youporn"},
	{[]string{"brazzers.com", "brazzer.com", "brazzers.xxx", "brazzersnetwork.com", "mofos.com"}, "brazzers", "Brazzers", "brazzers.com", "brazzers"},
	{[]string{"onlyfans.com"}, "onlyfans", "OnlyFans", "onlyfans.com", "onlyfans"},
	{[]string{"facebook.com", "fbcdn.net", "fb.com", "fbsbx.com", "facebook.net", "fb.watch", "fb.me", "fb.gg", "accountkit.com", "atscaleconference.com", "botorch.org", "buck.build", "buckbuild.com", "componentkit.org", "draftjs.org", "f8.com", "facebookappcenter.info", "facebookappcenter.net", "facebookappcenter.org", "facebookconnect.com", "facebookdevelopergarage.com", "faciometrics.com", "fasttext.cc", "fbf8.com", "fbinfer.com", "fblitho.com", "fbredex.com", "fbrell.com", "flow.dev", "flow.org", "flowtype.org", "frescolib.org", "hacklang.org", "hhvm.com", "makeitopen.com", "mcrouter.net", "mcrouter.org", "messengerdevelopers.com", "ogp.me", "opengraphprotocol.com", "opengraphprotocol.org", "parse.com", "pyrobot.org", "react.com", "reactjs.com", "reactjs.org", "recoiljs.org", "rocksdb.com", "rocksdb.net", "rocksdb.org", "yogalayout.com"}, "facebook", "Facebook", "facebook.com", "facebook"},
	{[]string{"instagram.com", "cdninstagram.com", "instagr.am", "achat-followers-instagram.com", "acheter-followers-instagram.com", "acheterdesfollowersinstagram.com", "acheterfollowersinstagram.com", "bookstagram.com", "carstagram.com", "chickstagram.com", "ig.me", "igcdn.com", "igsonar.com", "igtv.com", "imstagram.com", "imtagram.com", "instaadder.com", "instachecker.com", "instafallow.com", "instafollower.com", "instagainer.com", "instagda.com", "instagify.com", "instagmania.com", "instagor.com", "instagram-brand.com", "instagram-engineering.com", "instagram-help.com", "instagram-press.com", "instagram-press.net", "instagramci.com", "instagramcn.com", "instagramdi.com", "instagramhashtags.net", "instagramhilecim.com", "instagramhilesi.org", "instagramium.com", "instagramizlenme.com", "instagramkusu.com", "instagramlogin.com", "instagramm.com", "instagramn.com", "instagrampartners.com", "instagramphoto.com", "instagramq.com", "instagramsepeti.com", "instagramtakipcisatinal.net", "instagramtakiphilesi.com", "instagramtips.com", "instagramtr.com", "instagran.com", "instagranm.com", "instagrem.com", "instagrm.com", "instagtram.com", "instagy.com", "instamgram.com", "instangram.com", "instaplayer.net", "instastyle.tv", "instgram.com", "intagram.com", "intagrm.com", "intgram.com", "kingstagram.com", "lnstagram-help.com", "oninstagram.com", "online-instagram.com", "onlineinstagram.com", "theinstagramhack.com", "web-instagram.net", "wwwinstagram.com"}, "instagram", "Instagram", "instagram.com", "instagram"},
	{[]string{"x.com", "twitter.com", "twimg.com", "t.co", "ads-twitter.com", "cms-twdigitalassets.com", "periscope.tv", "pscp.tv", "tellapart.com", "tweetdeck.com", "twitpic.com", "twitter.biz", "twitter.jp", "twittercommunity.com", "twitterflightschool.com", "twitterinc.com", "twitteroauth.com", "twitterstat.us", "twtrdns.net", "twttr.com", "twttr.net", "twvid.com", "vine.co"}, "x", "X", "x.com", "x"},
	{[]string{"whatsapp.com", "whatsapp.net", "wa.me", "whatsapp-plus.info", "whatsapp-plus.me", "whatsapp-plus.net", "whatsapp.cc", "whatsapp.info", "whatsapp.org", "whatsapp.tv", "whatsappbrand.com"}, "whatsapp", "WhatsApp", "whatsapp.com", "whatsapp"},
	{[]string{"telegram.org", "t.me", "telegra.ph", "telegram.me", "telesco.pe", "cdn-telegram.org", "comments.app", "contest.com", "fragment.com", "graph.org", "quiz.directory", "tdesktop.com", "telega.one", "telegram-cdn.org", "telegram.dog", "telegram.space", "tg.dev", "ton.org", "toncenter.com", "tx.me", "usercontent.dev"}, "telegram", "Telegram", "telegram.org", "telegram"},
	{[]string{"discord.com", "discordapp.com", "discord.gg", "discordapp.net", "airhorn.solutions", "airhornbot.com", "bigbeans.solutions", "dis.gd", "discord-activities.com", "discord.co", "discord.design", "discord.dev", "discord.gift", "discord.gifts", "discord.media", "discord.new", "discord.store", "discord.tools", "discordactivities.com", "discordapp.io", "discordcdn.com", "discordmerch.com", "discordpartygames.com", "discordsays.com", "discordstatus.com", "hammerandchisel.ssl.zendesk.com", "watchanimeattheoffice.com"}, "discord", "Discord", "discord.com", "discord"},
	{[]string{"reddit.com", "redd.it", "reddit.app.link", "redditblog.com", "reddithelp.com", "redditinc.com", "redditmail.com", "redditmedia.com", "redditspace.com", "redditstatic.com", "redditstatus.com"}, "reddit", "Reddit", "reddit.com", "reddit"},
	{[]string{"pinterest.com", "pinimg.com", "pin.it", "pinimg.com.cdn.cloudflare.net", "pinterest.at", "pinterest.be", "pinterest.ca", "pinterest.ch", "pinterest.cl", "pinterest.co", "pinterest.co.at", "pinterest.co.in", "pinterest.co.kr", "pinterest.co.nz", "pinterest.co.uk", "pinterest.com.au", "pinterest.com.bo", "pinterest.com.ec", "pinterest.com.mx", "pinterest.com.pe", "pinterest.com.py", "pinterest.com.uy", "pinterest.com.vn", "pinterest.de", "pinterest.dk", "pinterest.ec", "pinterest.engineering", "pinterest.es", "pinterest.fr", "pinterest.hu", "pinterest.id", "pinterest.ie", "pinterest.in", "pinterest.info", "pinterest.it", "pinterest.jp", "pinterest.kr", "pinterest.mx", "pinterest.net", "pinterest.nl", "pinterest.nz", "pinterest.pe", "pinterest.ph", "pinterest.pt", "pinterest.ru", "pinterest.se", "pinterest.th", "pinterest.tw", "pinterest.uk", "pinterest.vn", "pinterestmail.com"}, "pinterest", "Pinterest", "pinterest.com", "pinterest"},
	{[]string{"snapchat.com", "sc-cdn.net", "snap.com"}, "snapchat", "Snapchat", "snapchat.com", "snapchat"},
	{[]string{"signal.org", "signal.art", "signal.group", "signal.link", "signal.me", "signal.tube", "signalusers.org", "whispersystems.org"}, "signal", "Signal", "signal.org", "signal"},
	{[]string{"threads.net", "threads.com"}, "threads", "Threads", "threads.net", "threads"},
	{[]string{"linkedin.com", "licdn.com", "bizographics.com", "l-0005.dc-msedge.net", "l-0005.l-msedge.net", "licdn.cn", "licdn.cn.cdn20.com", "linkedin.at", "linkedin.cn", "linkedin.sc.omtrdc.net", "lnkd.in"}, "linkedin", "LinkedIn", "linkedin.com", "linkedin"},
	{[]string{"skype.com"}, "skype", "Skype", "skype.com", "skype"},
	{[]string{"messenger.com", "fbmessenger.com", "m.me", "nbabot.net"}, "messenger", "Messenger", "messenger.com", "messenger"},
	{[]string{"tumblr.com"}, "tumblr", "Tumblr", "tumblr.com", "tumblr"},
	{[]string{"line.me", "line-scdn.net", "gcld-line.com", "lin.ee", "line-apps-beta.com", "line-apps-rc.com", "line-apps.com", "line-beta.me", "line-cdn.net", "line.biz", "line.naver.jp", "linecorp.com", "linefriends.com", "linefriends.com.tw", "linegame.jp", "linemobile.com", "linemyshop.com", "lineshoppingseller.com", "linetv.tw", "nhncorp.jp"}, "line", "LINE", "line.me", "line"},
	{[]string{"mastodon.social"}, "mastodon", "Mastodon", "mastodon.social", "mastodon"},
	{[]string{"bsky.app", "bsky.social", "bsky.network"}, "bluesky", "Bluesky", "bsky.app", "bluesky"},
	{[]string{"kakao.com", "kakaocdn.net", "1thek.com", "antenna.co.kr", "awesomeent.co.kr", "bhent.co.kr", "daum.net", "daumcdn.net", "daumkakao.io", "daumpcbang.com", "dolphiners.com", "fashionbykakao.com", "gleline.com", "istent.co.kr", "jwide.co.kr", "kakao.co.kr", "kakao.vc", "kakaobank.com", "kakaobank.io", "kakaobankcontent.com", "kakaobrain.com", "kakaocloud.com", "kakaocorp.com", "kakaoent.com", "kakaoenterprise.com", "kakaogamescorp.com", "kakaohealthcare.com", "kakaoilaas.com", "kakaoinvestment.com", "kakaomobility.com", "kakaopay.com", "kakaopaysec.com", "kakaopiccoma.com", "kakaostyle.com", "kakaotalk.jp", "kakaovx.com", "karechat.ai", "kgslb.com", "kpinsurances.com", "krosspictures.com", "lionhearts.co.kr", "logosfilm.co.kr", "megamon.co.kr", "melon.com", "metabora.io", "msoopent.com", "onkakao.net", "ootbstudio.co.kr", "pastahealth.com", "posty.kr", "sena.co.kr", "shownote.com", "starship-ent.com", "stuidok110.com", "vastenm.com", "zigzag.kr", "zipcine.com"}, "kakao", "KakaoTalk", "kakao.com", "kakao"},
	{[]string{"viber.com", "s-bid.rmp.rakuten.com", "vbcdn.com", "vbcdn.net", "viber.co", "viber.me"}, "viber", "Viber", "viber.com", "viber"},
	{[]string{"badoo.com"}, "badoo", "Badoo", "badoo.com", "badoo"},
	{[]string{"gab.com"}, "gab", "Gab", "gab.com", "gab"},
	{[]string{"parler.com"}, "parler", "Parler", "parler.com", "parler"},
	{[]string{"truthsocial.com"}, "truthsocial", "Truth Social", "truthsocial.com", "truthsocial"},
	{[]string{"clubhouse.com", "clubhouseapi.com", "joinclubhouse.com"}, "clubhouse", "Clubhouse", "clubhouse.com", "clubhouse"},
	{[]string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "browser-intake-datadoghq.com", "chat.com", "chatgpt.livekit.cloud", "chatgpt.site", "crixet.com", "host.livekit.cloud", "o33249.ingest.sentry.io", "o33249.ingest.us.sentry.io", "oaistatsig.com", "openai.com.cdn.cloudflare.net", "openai.org", "openai.qualtrics.com", "openaicom-api-bdcpf8c6d2e9atf6.z01.azurefd.net", "openaicom.imgix.net", "sora.com", "turn.livekit.cloud"}, "openai", "OpenAI", "openai.com", "openai"},
	{[]string{"claude.ai", "anthropic.com", "ant.dev", "anthropic.team", "antspace.dev", "chatbotclaude.com", "clau.de", "claude.app", "claude.com", "claude.dev", "claude.new", "claude.site", "claudeacademy.com", "claudemcpclient.com", "claudemcpcontent.com", "claudestudio.com", "claudeusercontent.com", "servd-anthropic-website.b-cdn.net"}, "claude", "Claude", "claude.ai", "claude"},
	{[]string{"deepseek.com", "deepseeksvc.com"}, "deepseek", "DeepSeek", "deepseek.com", "deepseek"},
	{[]string{"grok.com", "x.ai", "grokipedia.com"}, "grok", "Grok", "grok.com", "grok"},
	{[]string{"midjourney.com"}, "midjourney", "Midjourney", "midjourney.com", "midjourney"},
	{[]string{"perplexity.ai", "perplexity.com", "pplx-res.cloudinary.com", "pplx.ai"}, "perplexity", "Perplexity", "perplexity.ai", "perplexity"},
	{[]string{"mistral.ai"}, "mistral", "Mistral AI", "mistral.ai", "mistral"},
	{[]string{"huggingface.co", "hf.co", "hf.space"}, "huggingface", "Hugging Face", "huggingface.co", "huggingface"},
	{[]string{"cohere.com"}, "cohere", "Cohere", "cohere.com", "cohere"},
	{[]string{"cursor.com", "cursor-cdn.com", "cursor.sh", "cursorapi.com", "cursorvm.com"}, "cursor", "Cursor", "cursor.com", "cursor"},
	{[]string{"meta.ai", "meta.com", "metaaivm.com"}, "meta", "Muse", "meta.ai", "meta"},
	{[]string{"suno.com"}, "suno", "Suno", "suno.com", "suno"},
	{[]string{"elevenlabs.io", "elevenlabs.com"}, "elevenlabs", "ElevenLabs", "elevenlabs.io", "elevenlabs"},
	{[]string{"replicate.com"}, "replicate", "Replicate", "replicate.com", "replicate"},
	{[]string{"character.ai"}, "characterai", "Character.AI", "character.ai", "characterai"},
	{[]string{"runwayml.com"}, "runway", "Runway", "runwayml.com", "runway"},
	{[]string{"stability.ai"}, "stablediffusion", "Stable Diffusion", "stability.ai", "stablediffusion"},
	{[]string{"spotify.com", "scdn.co", "spoti.fi", "byspotify.com", "cdn-spotify-experiments.conductrics.com", "lifeatspotify.com", "pscdn.co", "spotify-everywhere.com", "spotify.design", "spotify.link", "spotifycdn.com", "spotifycdn.net", "spotifycharts.com", "spotifycodes.com", "spotifyforbrands.com", "spotifyforvendors.com", "spotifyjobs.com", "tospotify.com"}, "spotify", "Spotify", "spotify.com", "spotify"},
	{[]string{"soundcloud.com", "sndcdn.com", "soundcloud.cloud"}, "soundcloud", "SoundCloud", "soundcloud.com", "soundcloud"},
	{[]string{"tidal.com", "tidalhifi.com", "wimpmusic.com"}, "tidal", "Tidal", "tidal.com", "tidal"},
	{[]string{"deezer.com", "dzcdn.net"}, "deezer", "Deezer", "deezer.com", "deezer"},
	{[]string{"pandora.com"}, "pandora", "Pandora", "pandora.com", "pandora"},
	{[]string{"bandcamp.com", "bcbits.com"}, "bandcamp", "Bandcamp", "bandcamp.com", "bandcamp"},
	{[]string{"last.fm", "ws.audioscrobbler.com"}, "lastfm", "Last.fm", "last.fm", "lastfm"},
	{[]string{"iheartradio.com"}, "iheartradio", "iHeartRadio", "iheartradio.com", "iheartradio"},
	{[]string{"tunein.com"}, "tunein", "TuneIn", "tunein.com", "tunein"},
	{[]string{"audius.co"}, "audius", "Audius", "audius.co", "audius"},
	{[]string{"shazam.com"}, "shazam", "Shazam", "shazam.com", "shazam"},
	{[]string{"steampowered.com", "steamcommunity.com", "steamstatic.com", "steamcontent.com", "a4e8s8k3.map2.ssl.hwcdn.net", "client-update.queniuqe.com", "csgo.com.cn", "csgo.wmsj.cn", "dl.steam.clngaa.com", "dota2.com", "dota2.com.cn", "dota2.wmsj.cn", "edge.steam-dns.top.comcast.net", "f3b7q2p3.ssl.hwcdn.net", "gstore.val.manlaxy.com", "lv.queniujq.cn", "playartifact.com", "s.team", "st.dl.bscstorage.net", "st.dl.eccdnx.com", "st.dl.pinyuncloud.com", "steam-api.com", "steam-chat.com", "steam.apac.qtlglb.com", "steam.cdn.on.net", "steam.cdn.orcon.net.nz", "steam.cdn.slingshot.co.nz", "steam.cdn.webra.ru", "steam.eca.qtlglb.com", "steam.naeu.qtlglb.com", "steam.ru.qtlglb.com", "steam.tv", "steamchina.com", "steamconnecttest.com", "steamdeck.com", "steamgames.com", "steamos.cloud", "steampowered.com.8686c.com", "steamserver.net", "steamstatic.com.8686c.com", "steamusercontent.com", "underlords.com", "valve.net", "wmsjsteam.com", "xz.pphimalayanrt.com"}, "steam", "Steam", "steampowered.com", "steam"},
	{[]string{"epicgames.com"}, "epic", "Epic Games", "epicgames.com", "epic"},
	{[]string{"playstation.com", "playstation.net", "playstationnetwork.com", "sonyentertainmentnetwork.com"}, "playstation", "PlayStation", "playstation.com", "playstation"},
	{[]string{"xbox.com", "xboxlive.com", "callersbane.com", "forza.net", "forzamotorsport.net", "forzaracingchampionship.com", "forzarc.com", "gamepass.com", "minecraft-services.net", "minecraftservices.com", "minecraftshop.com", "mojang.com", "orithegame.com", "renovacionxboxlive.com", "tellmewhygame.com", "xbox.co", "xbox.eu", "xbox.org", "xbox360.co", "xbox360.com", "xbox360.eu", "xbox360.org", "xboxab.com", "xboxgamepass.com", "xboxgamestudios.com", "xboxlive.cn", "xboxone.co", "xboxone.com", "xboxone.eu", "xboxplayanywhere.com", "xboxservices.com", "xboxstudios.com", "xbx.lv"}, "xbox", "Xbox", "xbox.com", "xbox"},
	{[]string{"roblox.com", "rbxcdn.com", "blox.com", "goblocks.com", "goblox.com", "gooblox.com", "humancoexperience.com", "rblx.co", "rblx.com", "rblx.org", "rbx.com", "rbxinfra.net", "rbxlabs.com", "rbxtrk.com", "ro-blox.com", "roadblocks.com", "roblocks.com", "roblox-api.arkoselabs.com", "roblox.ca", "roblox.cc", "roblox.cl", "roblox.co.jp", "roblox.co.uk", "roblox.de", "roblox.dk", "roblox.eu", "roblox.fr", "roblox.hu", "roblox.it", "roblox.jp", "roblox.kr", "roblox.mx", "roblox.net", "roblox.org", "roblox.ph", "roblox.pt", "roblox.sk", "roblox.tv", "roblox.uk", "roblox.us", "roblox.vn", "robloxcorp.s.llnwi.net", "robloxlabs.com", "rowblocks.com", "rowblox.com"}, "roblox", "Roblox", "roblox.com", "roblox"},
	{[]string{"riotgames.com"}, "riotgames", "Riot Games", "riotgames.com", "riotgames"},
	{[]string{"battle.net"}, "battlenet", "Battle.net", "battle.net", "battlenet"},
	{[]string{"ea.com", "anthemgame.com", "anthemthegame.com", "apexlegends.com", "awayoutgame.com", "battlefield.com", "battlefield1943.com", "battlefield3.com", "battlefield4.com", "battlefield5.com", "battlefieldbadcompany2.com", "battlefieldheroes.com", "battlefieldv.com", "battlefront2.com", "battlefrontii.com", "battlelog.com", "bejeweledstars.com", "bejewled-stars.com", "bioware.com", "biowarestore.com", "blackboxgames.com", "camelot-europe.com", "camelotherald.com", "camelotherald.net", "capitalgames.com", "chillingo.com", "cncrivals.com", "commandandconquer.com", "conquerwithcharacter.com", "crysis.jp", "daoc.net", "darkageofcamelot.com", "darkness-risen.com", "dawngate.com", "dawngatechronicles.com", "deadspacegame.com", "dice.se", "dicela.com", "dicela.net", "dragonage.com", "dragonagekeep.com", "dragonagemovie.com", "dungeonkeeper.cn", "dungeonkeeper.com", "dungeonkeeper.com.cn", "ea-anz-press.com", "ea.tt.omtrdc.net", "eaaccess.com", "eablackbox.com", "eacashcard.com", "eacodigos.com", "eafootballworld.com", "eakorea.co.kr", "eamirrorsedge.com", "eamobile.com", "eamythic.com", "eamythic.net", "eanordic.com", "eaplay.com", "easports.com", "easports.jp", "easportsactive.com", "easportsactiveonline.com", "easportsfootball.com", "easportsfootballclub.com", "easportsmma.com", "easportsworld.com", "eastore.com", "electronicarts.com", "electronicarts.fr", "fifastreet.com", "fifastreet3.com", "frostbite.com", "futpromos.com", "futunited.com", "ghostgames.com", "heroesofdragonage.com", "hutpromos.com", "industrialtoys.com", "lordofultima.com", "maddenchampionship.com", "maddenrewards.com", "maddenseason.info", "maddenseason.net", "maddenseason.org", "masseffect.com", "masseffectarchives.com", "maxis.com", "mirrorsedge.com", "mirrorsedge.jp", "mirrorsedge.net", "mirrorsedge2.com", "mirrorsedge2d.com", "mysims.com", "mysimsracing.com", "mythicentertainment.com", "mythicentertainment.net", "mythicgames.com", "needforspeed.com", "needforspeedboost.com", "needforspeeddriftkings.com", "needforspeedeliminator.com", "needforspeedlightning.com", "needforspeedoverdrive.com", "needforspeedproven.com", "needforspeedredline.com", "needforspeedshowdown.com", "needforspeedstreetkings.com", "needforspeedtakedown.com", "needforspeedtherun.com", "needforspeedtimeattack.com", "needforspeedundergroundeast.com", "nfsworld.com", "plantsvszombies2.com", "play4free.com", "playapex.com", "pogo.com", "pogobeta.com", "popcap.com", "projectapex.com", "pvzgw2.com", "pvzheroes.com", "rtm.tnt-ea.com", "seaofsolitude.com", "simcity-buildit.com", "simcity.com", "skate2.com", "spearhead.kr", "speedhunters.com", "spore.com", "ssx3.com", "starwarsbattlefront.com", "starwarsbattlefront2.com", "starwarsfallenorder.com", "starwarsjedifallenorder.com", "starwarstheoldrepublic.com", "swjedifallenorder.com", "swjfo.com", "swtor.com", "swtor.net", "teamneedforspeed.com", "thedreadwolfrises.com", "thesims.com", "thesims3.com", "thesims4.com", "thesimssocial.com", "tiberiumalliances.com", "tiburon.com", "titanfall.com", "tnt-ea.com", "ultimaforever.com", "ultimaonline.com", "unravel2.com", "unraveltwo.com", "uo.com", "uoherald.com", "visceralgames.com", "xdsummit.com", "yogify.com"}, "ea", "EA", "ea.com", "ea"},
	{[]string{"ubisoft.com", "ubi.com", "ubisoft.siteintercept.qualtrics.com", "ubisoftconnect.com", "uplay.com"}, "ubisoft", "Ubisoft", "ubisoft.com", "ubisoft"},
	{[]string{"rockstargames.com"}, "rockstargames", "Rockstar Games", "rockstargames.com", "rockstargames"},
	{[]string{"gog.com", "gog-cdn-lumen.secure2.footprint.net", "gog-services.com", "gog-statics.com", "gog.qtlglb.com", "gogalaxy.com"}, "gog", "GOG", "gog.com", "gog"},
	{[]string{"itch.io"}, "itch", "itch.io", "itch.io", "itch"},
	{[]string{"humblebundle.com"}, "humble", "Humble Bundle", "humblebundle.com", "humble"},
	{[]string{"origin.com", "origin.tv"}, "origin", "Origin", "origin.com", "origin"},
	{[]string{"valvesoftware.com"}, "valve", "Valve", "valvesoftware.com", "valve"},
	{[]string{"hoyoverse.com"}, "hoyoverse", "HoYoverse", "hoyoverse.com", "hoyoverse"},
	{[]string{"supercell.com", "boombeach.com", "brawlstars.com", "brawlstarsgame.com", "clash.com", "clashmini.com", "clashofclans.com", "clashroyale.com", "clashroyaleapp.com", "gamesfirsthelsinki.com", "hayday.com", "haydaygame.com", "mo.co", "seeurlpcl.com", "squadbusters.com", "supercell.helpshift.com", "supercell.net", "supercell.zendesk.com", "supercellcreators.com", "supercellgames.com", "supercellid.com", "supercellstore.com", "supr.cl"}, "supercell", "Supercell", "supercell.com", "supercell"},
	{[]string{"zynga.com", "zyngaplayersupport.com", "zyngapoker.com"}, "zynga", "Zynga", "zynga.com", "zynga"},
	{[]string{"activision.com", "callofduty.com", "callofdutyendowment.com", "callofdutyendowment.org", "callofdutyleague.com", "callofdutymobile.com", "crashbandicoot.com", "demonware.net", "highmoonstudios.com", "infinityward.com", "ravensoftware.com", "sekirothegame.com", "sierragames.com", "skylanders.com", "sledgehammergames.com", "solidstate.com", "spyrothedragon.com", "tonyhawk.com", "tonyhawkthegame.com", "treyarch.com", "worldseriesofwarzone.com"}, "activision", "Activision", "activision.com", "activision"},
	{[]string{"bethesda.net", "beth.games", "bethesdagamestudios.com", "bethsoft.com", "doom.com", "elderscrolls.com"}, "bethesda", "Bethesda", "bethesda.net", "bethesda"},
	{[]string{"square-enix.com"}, "squareenix", "Square Enix", "square-enix.com", "squareenix"},
	{[]string{"atari.com"}, "atari", "Atari", "atari.com", "atari"},
	{[]string{"nintendo.com", "nintendo.net", "100classicbooks.com", "ac-pocketcamp.com", "amiibo.com", "ayudanintendo.com", "bowsersinsidestory.com", "dialga.com", "diddykongracing.com", "dkr.com", "donkeykongcountryreturns.com", "drmario-world.com", "excitebots.com", "fire-emblem-heroes.com", "fireemblemawakening.com", "flipnotestudio.com", "giratina.com", "gloryofheracles.com", "hackyourconsole.com", "kirbysepicyarn.com", "kirbysuperstarultra.com", "kyurem.com", "legendofzelda.cn", "legendofzelda.com.cn", "marioandluigidreamteam.com", "mariobroswii.com", "mariokart.cn", "mariokart.com.cn", "mariokart7.com", "mariokart8.com", "mariosupersluggers.com", "miitomo.com", "miitomo.com.cn", "miiverse.com", "nentindo.net", "newsupermariobrosu.com", "nintendo-europe-sales.com", "nintendo-europe.com", "nintendo.at", "nintendo.be", "nintendo.ch", "nintendo.co.jp", "nintendo.co.kr", "nintendo.co.uk", "nintendo.co.za", "nintendo.com.hk", "nintendo.com.pt", "nintendo.de", "nintendo.dk", "nintendo.es", "nintendo.eu", "nintendo.fi", "nintendo.fr", "nintendo.it", "nintendo.nl", "nintendo.no", "nintendo.pt", "nintendo.ru", "nintendo.se", "nintendo.tw", "nintendo3ds.com", "nintendodsi.com", "nintendoeurope.com", "nintendolabo.cn", "nintendonetwork.net", "nintendonyc.com", "nintendostore.com", "nintendoswitch.cn", "nintendoswitch.com", "nintendoswitch.com.cn", "nintendoswitch.net", "nintendoswitchtogether.com", "nintendowii.com", "personaltrainermath.com", "playnintendo.com", "pokedex3d.com", "pokemon-moon.com", "pokemon-sun.com", "pokemon-sunmoon.com", "pokemon-unitepgame.com", "pokemon.com", "pokemonbank.com", "pokemonblackwhite.com", "pokemonbw.com", "pokemonchampionships.com", "pokemongoldsilver.com", "pokemonhome.com", "pokemonletsgoeevee.com", "pokemonletsgopikachu.com", "pokemonmysterydungeon.com", "pokemonpicross.com", "pokemonplatinum.com", "pokemonrubysapphire.com", "pokemonsunmoon.com", "pokemonswordshield.com", "pokemonultrasunmoon.com", "pokemonvgc.com", "pokemonwifi.net", "splatoon2tournament.com", "starfox.com", "supermario.com", "supermario3dworld.com", "supermariobros.com.cn", "supermariogalaxy.com", "supermariorun.com", "superpapermario.com", "supersmashbros.cn", "supersmashbros.com.cn", "thelegendarystarfy.com", "thewonderful101.com", "wariolandshakeit.com", "wariowarediy.com", "wii-u.com", "wiifit.com", "wiifitu.com", "wiipartyu.com", "wiisports.com", "wiisportsresort.com", "wiiugamepad.com", "wiivc.net", "xenoblade.com", "xn--mts47c3w9b1qr.cn", "xn--mts47c3w9b1qr.net", "yoshisnewisland.com"}, "nintendo", "Nintendo", "nintendo.com", "nintendo"},
	{[]string{"minecraft.net"}, "minecraft", "Minecraft", "minecraft.net", "minecraft"},
	{[]string{"google.com", "googleapis.com", "gstatic.com", "gmail.com", "googleusercontent.com", "ggpht.com", "doubleclick.net", "googlesyndication.com", "googleadservices.com", "googletagmanager.com", "googlesource.com", "ai.google.dev", "ai.studio", "antigravity-unleash.goog", "antigravity.google", "deepmind.com", "deepmind.google", "flow.google", "gemini.google", "generativeai.google", "jules.google", "labs.google", "notebook.google", "notebooklm.google", "opal.google", "stitch.withgoogle.com"}, "google", "Google", "google.com", "google"},
	{[]string{"notion.so", "notion.site", "notion-static.com", "notion-status.com", "notion.com", "notion.new", "notionusercontent.com"}, "notion", "Notion", "notion.so", "notion"},
	{[]string{"slack.com", "slack-core.com", "slack-edge.com", "slack-files.com", "slack-imgs.com", "slack-msgs.com", "slack-redir.net", "slackb.com", "slackcertified.com", "slackdemo.com", "slackhq.com"}, "slack", "Slack", "slack.com", "slack"},
	{[]string{"zoom.us", "zoom.com", "zoom.com.cn"}, "zoom", "Zoom", "zoom.us", "zoom"},
	{[]string{"dropbox.com", "dropboxusercontent.com", "addtodropbox.com", "dash.ai", "db.tt", "docsend.com", "dropbox-dns.com", "dropbox.tech", "dropbox.zendesk.com", "dropboxapi.com", "dropboxbusiness.com", "dropboxcaptcha.com", "dropboxexperiment.com", "dropboxforum.com", "dropboxforums.com", "dropboxinsiders.com", "dropboxlegal.com", "dropboxmail.com", "dropboxpartners.com", "dropboxstatic.com", "dropboxteam.com", "getdropbox.com", "hellofax.com", "hellosign.com"}, "dropbox", "Dropbox", "dropbox.com", "dropbox"},
	{[]string{"box.com"}, "box", "Box", "box.com", "box"},
	{[]string{"github.com", "githubusercontent.com", "github.io", "raw.githubusercontent.com", "githubassets.com", "copilot-workspace.githubnext.com", "githubcopilot.com"}, "github", "GitHub", "github.com", "github"},
	{[]string{"gitlab.com", "gitlab-assets.oss-cn-hongkong.aliyuncs.com", "gitlab-static.net", "gitlab.io", "gitlab.net"}, "gitlab", "GitLab", "gitlab.com", "gitlab"},
	{[]string{"stackoverflow.com", "stackexchange.com", "sstatic.net", "askubuntu.com", "blogoverflow.com", "codeforaliving.io", "mathoverflow.net", "serverfault.com", "stackapps.com", "stackauth.com", "stackmod.blog", "stackoverflow.blog", "stackoverflow.co", "stackoverflow.email", "stackoverflow.help", "stackoverflow.net", "stackoverflowbusiness.com", "stackoverflowcareers.com", "stackoverflowjobs.com", "stackoverflowsolutions.com", "stackoverflowteams.com", "stacksnippets.net", "superuser.com", "tex-talk.net", "thesffblog.com"}, "stackoverflow", "Stack Overflow", "stackoverflow.com", "stackoverflow"},
	{[]string{"wikipedia.org", "wikimedia.org", "mediawiki.org", "toolforge.org", "w.wiki", "wikibooks.org", "wikidata.org", "wikimediacloud.org", "wikimediafoundation.org", "wikinews.org", "wikiquote.org", "wikisource.org", "wikiversity.org", "wikivoyage.org", "wiktionary.org", "wmcloud.org", "wmflabs.org", "wmfusercontent.org"}, "wikipedia", "Wikipedia", "wikipedia.org", "wikipedia"},
	{[]string{"figma.com"}, "figma", "Figma", "figma.com", "figma"},
	{[]string{"adobe.com", "10xfotolia.com", "2o7.net", "acrobat.com", "adbecrsl.com", "adobe-aemassets-value.com", "adobe-audience-finder.com", "adobe-video-partner-finder.com", "adobe.io", "adobe.ly", "adobeaemcloud.com", "adobeaemcloud.net", "adobeawards.com", "adobecc.com", "adobecce.com", "adobeccstatic.com", "adobecontent.io", "adobecreativityawards.com", "adobedc.cn", "adobedc.net", "adobedemo.com", "adobedtm.com", "adobeereg.com", "adobeexchange.com", "adobeexperienceawards.com", "adobegov.com", "adobehiddentreasures.com", "adobejanus.com", "adobeku.com", "adobelanding.com", "adobelogin.com", "adobeoobe.com", "adobeplatinumclub.com", "adobeprojectm.com", "adobesc.com", "adobesign.com", "adobesigncdn.com", "adobespark.com", "adobess.com", "adobestats.io", "adobestock.com", "adobetag.com", "adobetarget.com", "adobetcstrialdvd.com", "adobetechcomm.com", "adobetechcommcallback.com", "adobetechcommdemo.com", "adobexdplatform.com", "assetsadobe.com", "authorxml.com", "bluefootcms.com", "businesscatalyst.com", "ccnsite.com", "ccpsx.com", "compresspdf.new", "cotolia.com", "creativecloud.com", "creativesdk.com", "demdex.net", "developria.com", "dollarfotoclub.com", "dollarphotoclub.com", "dollarphotosclub.com", "douwriteright.com", "echocdn.com", "echosign.com", "edgefonts.net", "enablementadobe.com", "ffotolia.com", "fiotolia.com", "foftolia.com", "fonolia.com", "fotiolia.com", "fotoiia.com", "fotolia-noticias.com", "fotolia.cc", "fotolia.com", "fotolia.tv", "fotolja.com", "fptolia.com", "ftcdn.net", "gfotolia.com", "gostorego.com", "imagineecommerce.com", "macromedia.com", "mageconf.com", "mageconf.com.ua", "magento.com", "magento.net", "magentocommerce.com", "magentoliveconference.com", "magentomobile.com", "marketing-cloud.com", "marketing-nirvana.com", "marketo.co.uk", "marketo.com", "marketo.net", "marketo.tv", "marketodesigner.com", "marketolive.com", "mktdns.com", "mkto-c0100.com", "mktorest.com", "mktroute.com", "mobilemarketo.com", "motolia.com", "omniture.com", "omtrdc.net", "ood.opsource.net", "pdf.new", "photolia.net", "photoshop.com", "placesdocs.com", "revenue-performance-management.com", "s2stagehance.com", "scene7.com", "sign.new", "sundanceignite2016.com", "tenbyfotolia.com", "toutapp.com", "tubemogul.com", "typekit.com", "typekit.net", "votolia.com", "worldsecureemail.com", "worldsecuresystems.com"}, "adobe", "Adobe", "adobe.com", "adobe"},
	{[]string{"microsoft.com", "office.com", "outlook.com", "live.com", "appcenter.ms", "microsoftonline.com", "windowsupdate.com", "msn.com", "bing.com", "sharepoint.com", "windows.com", "microsoft365.com", "hotmail.com", "onedrive.com", "azure.com", "visualstudio.com", "vsassets.io", "adaptivecards.io", "api-extractor.com", "apisof.net", "az764295.vo.msecnd.net", "bingsettingssearch.trafficmanager.net", "blazor.net", "botframework.com", "codethemicrobit.com", "default.exp-tas.com", "devopsassessment.net", "dot.net", "dtlgalleryint.cloudapp.net", "gamesstack.com", "graphengine.io", "insiderdevtour.com", "jwt.ms", "microbit.org", "microsoftadc.com", "microsoftgamestack.com", "microsoftiotinsiderlabs.com", "microsoftreactor.cn", "microsoftreactor.com.cn", "microsoftreactor.info", "microsoftreactor.net", "microsoftreactor.org", "microsoftsilverlight.com", "microsoftsilverlight.net", "microsoftsilverlight.org", "microsoftsqlserver.com", "mmdnn.com", "mono-project.com", "msdn.com", "msinnovationchallenge.com", "mspairlift.com", "msropendata.com", "nuget.org", "nugettest.org", "opentranslatorstothings.org", "poshtestgallery.cloudapp.net", "poshtestgallery.com", "psg-int-centralus.cloudapp.net", "psg-int-eastus.cloudapp.net", "pwabuilder.com", "reactorms.com.cn", "rink.hockeyapp.net", "sankie.net", "sqlserveronlinux.com", "timelinestoryteller.com", "uwpcommunitytoolkit.com", "vfsforgit.com", "vfsforgit.org", "visualstudio-staging.com", "visualstudio.co", "visualstudio.co.uk", "visualstudio.eu", "visualstudio.net", "vscode-cdn.net", "vscode-sync-insiders.trafficmanager.net", "vscode-sync.trafficmanager.net", "vscode-unpkg.net", "vscode-webview.net", "vscode.dev", "vscodeedu.app", "vscodeedu.com", "vsmarketplacebadge.apphb.com", "xamarin.com"}, "microsoft", "Microsoft", "microsoft.com", "microsoft"},
	{[]string{"apple.com", "icloud.com", "cdn-apple.com", "mzstatic.com", "icloud-content.com", "tv.applemusic.com"}, "apple", "Apple", "apple.com", "apple"},
	{[]string{"evernote.com"}, "evernote", "Evernote", "evernote.com", "evernote"},
	{[]string{"asana.com"}, "asana", "Asana", "asana.com", "asana"},
	{[]string{"trello.com", "trellocdn.com"}, "trello", "Trello", "trello.com", "trello"},
	{[]string{"monday.com"}, "monday", "Monday.com", "monday.com", "monday"},
	{[]string{"clickup.com"}, "clickup", "ClickUp", "clickup.com", "clickup"},
	{[]string{"airtable.com"}, "airtable", "Airtable", "airtable.com", "airtable"},
	{[]string{"miro.com"}, "miro", "Miro", "miro.com", "miro"},
	{[]string{"todoist.com"}, "todoist", "Todoist", "todoist.com", "todoist"},
	{[]string{"grammarly.com"}, "grammarly", "Grammarly", "grammarly.com", "grammarly"},
	{[]string{"nordvpn.com", "nordcdn.com"}, "nordvpn", "NordVPN", "nordvpn.com", "nordvpn"},
	{[]string{"overleaf.com"}, "overleaf", "Overleaf", "overleaf.com", "overleaf"},
	{[]string{"proton.me", "protonmail.com"}, "proton", "Proton", "proton.me", "proton"},
	{[]string{"tuta.com", "keemail.me", "tuta.io", "tutamail.com", "tutanota.com", "tutanota.de"}, "tutanota", "Tuta", "tuta.com", "tutanota"},
	{[]string{"mega.nz", "mega.co.nz", "mega.io", "megaad.nz", "megapay.nz", "megas4.com", "transfer.it"}, "mega", "MEGA", "mega.nz", "mega"},
	{[]string{"fastmail.com"}, "fastmail", "Fastmail", "fastmail.com", "fastmail"},
	{[]string{"canva.com", "affinity.api.serifservices.com", "affinity.studio", "canvastatus.com"}, "canva", "Canva", "canva.com", "canva"},
	{[]string{"expressvpn.com"}, "expressvpn", "ExpressVPN", "expressvpn.com", "expressvpn"},
	{[]string{"surfshark.com"}, "surfshark", "Surfshark", "surfshark.com", "surfshark"},
	{[]string{"protonvpn.com"}, "protonvpn", "Proton VPN", "protonvpn.com", "protonvpn"},
	{[]string{"mullvad.net"}, "mullvad", "Mullvad", "mullvad.net", "mullvad"},
	{[]string{"amazon.com", "media-amazon.com", "a2z.com", "aboutamazon.cn", "aboutamazon.co.uk", "aboutamazon.com", "aboutamazon.com.au", "aboutamazon.de", "aboutamazon.es", "aboutamazon.eu", "aboutamazon.fr", "aboutamazon.in", "aboutamazon.it", "aboutamazon.jp", "aboutamazon.pl", "aboutamazon.sg", "alexa.com", "alexafund.cn", "alexafund.com.cn", "amaaozn.com", "amazon-adsystem.com", "amazon-fashions.com", "amazon-jp-recruiting.com", "amazon-lantern.com", "amazon-launchpad.com", "amazon.ae", "amazon.ca", "amazon.cl", "amazon.cn", "amazon.co.jp", "amazon.co.uk", "amazon.co.za", "amazon.com.au", "amazon.com.be", "amazon.com.br", "amazon.com.co", "amazon.com.mx", "amazon.com.ng", "amazon.com.tr", "amazon.de", "amazon.dev", "amazon.eg", "amazon.es", "amazon.fr", "amazon.ie", "amazon.in", "amazon.it", "amazon.jobs", "amazon.jp", "amazon.lu", "amazon.nl", "amazon.pl", "amazon.red", "amazon.sa", "amazon.se", "amazon.sg", "amazonalexavoxcon.com", "amazonauthorinsights.com", "amazonbusiness.cn", "amazonbusiness.com.cn", "amazonbusiness.org", "amazonbusinessblog.com", "amazonchoice.cn", "amazonchoice.com.cn", "amazonchoices.cn", "amazonchoices.com.cn", "amazondevicesupport.com", "amazonfctours.com", "amazonianblog.com", "amazonimages.com", "amazoninspire.cn", "amazoninspire.com.cn", "amazonlaunchpad.cn", "amazonlaunchpad.com", "amazonlaunchpad.com.cn", "amazonlending.com.cn", "amazonliterarypartnership.com", "amazonlumberyard.wang", "amazonnow.cn", "amazonnow.com.cn", "amazonpay.com", "amazonpay.in", "amazonsdi.com", "amazonstudiosguilds.com", "amazontrust.com", "amazonvideodirect.com", "amzn.asia", "amzn.com", "amzn.to", "amznl.com", "assoc-amazon.cn", "associates-amazon.com", "audible.com", "awstrust.com", "createspace.com", "gameon-masters.com", "gdansk-amazon.com", "images-amazon.com", "images-cn-8.ssl-images-amazon.com", "images-cn.ssl-images-amazon.com", "images-eu.ssl-images-amazon.com", "images-fe.ssl-images-amazon.com", "images-na.ssl-images-amazon.com", "kindle.cn", "kindle.co.jp", "kindle.co.uk", "kindle.com", "kindle.de", "kindle.es", "kindle.fr", "kindle.in", "kindle.it", "kindle.jp", "kindleoasis.cn", "kindleoasis.com", "kindleoasis.com.cn", "kindleoasis.info", "kindleoasis.jp", "kindleoasis.org", "kindleoasis.us", "kindleoasisnews.com", "kindleproject.com", "msh.amazon.co.uk", "primeday.cn", "primeday.com.cn", "primeday.info", "prod.service.minerva.devices.a2z.com", "seattlespheres.com", "serving-sys.com", "siege-amazon.com", "ss2.us", "ssl-images-amazon.com", "static.siege-amazon.com", "ueberamazon.de", "wfm.com", "wholecitiesfoundation.org", "wholefoods.com", "wholefoodsmarket.co.uk", "wholefoodsmarket.com", "wholekidsfoundation.org", "wholeplanetfoundation.org", "yamaxun.cn", "yamaxun.com", "z.cn"}, "amazon", "Amazon", "amazon.com", "amazon"},
	{[]string{"ebay.com", "21centuryaccess.com", "4ebaytraders.com", "achievementanalytics.com", "adcommerce.cn", "adcommerce.tv", "appforebay.cn", "appsonebay.net", "asebay.com", "automobile.fr", "baazee.com", "bidbay.com", "bidorbuyindia.com", "billpoint.com", "billpoint.info", "billpoint.tv", "billpoint.us", "billpointnewzealand.com", "blogebay.com", "bookclubcorner.com", "builtfromebay.com", "buyitnow.com", "buyitnow.net", "buyitnow.org", "buyitnow.tv", "buyitnowshop.net", "cafr.ca", "carebay.com", "cargigileads.com", "cdn-terapeak.com", "cebay.com", "collective99.com", "commerceos.com", "connectcommerce.cn", "connectcommerce.com.cn", "connectcommerce.hk", "connectcommerce.info", "connectcommerce.tv", "connectedcommerce.cn", "connectedcommerce.com", "connectedcommerce.tv", "crececonebay.com", "creditcardsbay.com", "cyber-bay.cn", "cyber-bay.com.cn", "cyber-bay.info", "cyber-bay.org", "dba.dk", "dealbay.com", "dealtime.com", "didce.com", "douya.org", "dreamtoplay.com", "e-bay.com", "e-bay.it", "e-bay.net", "eachpay.com", "eachpay.net", "ebahy.com", "ebay-authenticate.net", "ebay-confirm.com", "ebay-course.com", "ebay-cz.com", "ebay-delivery.com", "ebay-discoveries.com", "ebay-fashion.com", "ebay-inc.com", "ebay-inc.net", "ebay-inc.org", "ebay-online.com", "ebay-sales.com", "ebay-stories.com", "ebay-us.com", "ebay-vacation.com", "ebay.at", "ebay.be", "ebay.ca", "ebay.ch", "ebay.cn", "ebay.co.nz", "ebay.co.uk", "ebay.co.ve", "ebay.co.za", "ebay.com.ar", "ebay.com.au", "ebay.com.cn", "ebay.com.ec", "ebay.com.hk", "ebay.com.mt", "ebay.com.my", "ebay.com.ph", "ebay.com.sg", "ebay.de", "ebay.es", "ebay.fr", "ebay.ie", "ebay.in", "ebay.it", "ebay.jp", "ebay.lt", "ebay.mn", "ebay.net.cn", "ebay.nl", "ebay.org", "ebay.org.cn", "ebay.ph", "ebay.pk", "ebay.pl", "ebay.sg", "ebay.us", "ebay.vn", "ebay.yn.cn", "ebay.zj.cn", "ebay25.com", "ebay68.com", "ebaya.com", "ebayads.com", "ebayads.net", "ebayadvertising.cn", "ebayadvertising.com", "ebayanunsios.net", "ebayauction.com", "ebayaustralia.com", "ebayauthenticate.com.cn", "ebaybags.com", "ebaybank.com", "ebaybenefits.com", "ebayboutique.com", "ebayca.com", "ebayca.org", "ebaycafe.com", "ebaycar.com", "ebaycareers.com", "ebaycbt.co.kr", "ebaycdn.net", "ebaychina.net", "ebayclassifieds.cn", "ebayclassifieds.com", "ebayclassifieds.com.cn", "ebayclassifieds.info", "ebayclassifieds.org", "ebayclassifieds.tv", "ebayclassifiedsgroup.com", "ebayclassifiedsgroup.info", "ebayclassifiedsgroup.org", "ebayclassifies.com", "ebayclub.com", "ebaycoins.com", "ebaycom.com", "ebaycommercenetwork.com", "ebaycourse.com", "ebayd.com", "ebayde.com", "ebaydesc.cn", "ebaydesc.com.cn", "ebaydlassifieds.com", "ebaydns.cn", "ebaydts.com", "ebayedu.com", "ebayeletro.com", "ebayenterprise.cn", "ebayenterprise.com", "ebayenterprise.com.cn", "ebayenterprise.info", "ebayenterprise.net", "ebayenterprise.tv", "ebayetc.com", "ebayexpress.sg", "ebayfashion.com", "ebayfashion.net", "ebayforcharity.org", "ebayforeclosure.org", "ebayfrance.com", "ebayglobalshipping.com", "ebaygroup.com", "ebayhabit.com", "ebayheels.com", "ebayhots.com", "ebayimg.com", "ebayinc.com", "ebayinc.net", "ebayinc.org", "ebayincconnectedcommerce.net", "ebayinkblog.com", "ebayinternetsalestax.com", "ebayit.com", "ebayjewelry.com", "ebayjob.com", "ebayla.org", "ebaylisting.com", "ebaylocal.net", "ebaylocationsdevacances.com", "ebaymag.com", "ebaymainstreet.com", "ebaymall.com", "ebaymarketplace.net", "ebaymotors.ca", "ebaymotors.cn", "ebaymotors.com", "ebaymotors.com.cn", "ebaymotors.org", "ebaymotorsblog.com", "ebaynow.com", "ebaynyc.com", "ebayon.com", "ebayon.net", "ebayoncampus.com", "ebayopen.com", "ebayopensource.com", "ebayopensource.net", "ebaypakistan.net", "ebaypark.com", "ebayparts.com", "ebaypedia.cn", "ebaypedia.com.cn", "ebayprivacycenter.com", "ebayqq.com", "ebayradio.com", "ebayrtm.com", "ebayseller.com", "ebayshoesstore.com", "ebayshop.com", "ebayshop111.com", "ebayshopping.cn", "ebayshopping.com.cn", "ebayshopping.org", "ebaysocial.com", "ebaysocial.ru", "ebaysoho.com", "ebaysohos.com", "ebaystatic.cn", "ebaystatic.com", "ebaystore.com", "ebaystore77.com", "ebaystores.cn", "ebaystyle.com", "ebaysweden.com", "ebayt.com", "ebaytechblog.com", "ebaytopratedseller.net", "ebaytrading.com", "ebaytradingassistant.com", "ebaytv.org", "ebayuae.net", "ebayvakantiehuizen.com", "ebayvalet.com", "ebayvietnam.net", "ebayworlds.com", "ebayy.com", "edisebay.com", "eebay.com", "epinions.com", "eu-consumer-empowerment.com", "expertmaker.com", "fairmarket.com", "fragrancebay.com", "francemail.com", "gittigidiyor.net", "gittigidiyorsikayet.com", "gumtree.sg", "half.com", "half.com.cn", "half.tv", "halfcanada.com", "halfjapan.com", "handbagsoutletebay.com", "iebay.com", "irribay.com", "itsbetterwhenyouwinit.com", "jijiji.ca", "kiji.ca", "kijii.ca", "kijij.ca", "kijiji.ca", "kijijiauto.ca", "kijijiautos.ca", "kijijidns.cn", "kijijidns.com.cn", "kijijiforbusiness.ca", "kijijii.ca", "kijijiraps.ca", "kijji.ca", "kijjiji.ca", "liketwice.com", "listinganalytics.net", "liveauction.com", "marketexecutive.net", "milofetch.com", "mobile.de", "mobileinternational.com", "mobileinternational.com.cn", "musicbay.net", "myconstructionworld.net", "myebay.com", "nebay.net", "nexcat.com", "nexpart.com", "nexpart.tv", "paisapay.cc", "paisapay.info", "paisapay.tv", "premobay.com", "privatemarketplaces.net", "privatemarketplaces.us", "prostores.cn", "prostores.com", "prostores.com.cn", "q10.jp", "qoo10.jp", "renchead.com", "rethink.net", "shopibay.net", "shoping.com", "sourcingforebay.com.cn", "sourcingforebay.net", "sourcingforebay.tv", "speybay.com", "storesense.com", "svpply.com", "tearapeak.com", "telebay.com", "telesell.com", "terapeack.com", "terapeak.ca", "terapeak.cn", "terapeak.com", "terapeak.com.hk", "terapeak.hk", "terapeak.info", "teraperk.com", "terrapeak.com", "texttobuy.org", "thebayuk.com", "theebayshop.com", "theopportunityproject.org", "towerauction.com", "vendu.com", "watch-ebay.org", "weareebay.com", "whisolutions.com", "wrenchead.com", "wwwdecide.com", "wwwebay.com", "wwwebay.net", "wwwwebay.com", "xindelu.com", "xn--3et96bj49ahpq.com", "xn--4vq475g.com", "xn--4vq477m.com", "xn--7hv594h.com", "xn--7hvy28f.cn", "xn--hb4aw0g.com", "xn--q41am8x.com", "xn--qoq462m.com", "xn--tkry91n.com", "xn--ubt498knmf.com", "xn--xsq421m.com", "xn--xsq605n.com", "xn--xsq959n.com", "xn--yf1at58a.com", "xxbay.com", "yibei.org"}, "ebay", "eBay", "ebay.com", "ebay"},
	{[]string{"paypal.com", "account-paypal.info", "account-paypal.net", "account-paypal.org", "accountpaypal.com", "accountpaypal.net", "accountpaypal.org", "anfutong.cn", "anfutong.com", "anfutong.com.cn", "beibao.cn", "beibao.com", "beibao.com.cn", "bill-safe.com", "billmelater.com", "billmelater.info", "billmelater.net", "bml.info", "braintreegateway.com", "braintreegateway.tv", "braintreepayments.com", "braintreepayments.info", "braintreepayments.org", "braintreepayments.tv", "braintreepaymentsolutions.com", "braintreeps.com", "briantreepayments.net", "briantreepayments.tv", "buyfast-paysmart.net", "card.io", "cash2.com", "cashify.com", "cashify.net", "devtools-paypal.com", "experiencebillmelater.com", "filipino-music.net", "fundpaypal.com", "getbraintree.com", "gmoney.org", "i-o-u.info", "krakenjs.com", "loanbuilder.com", "login-paypal.com", "login-paypal.info", "mywaytopay.info", "mywaytopay.net", "pa9pal.com", "paaypal.com", "paily.net", "paily.org", "paipal.com", "pavpal.com", "paydiant.com", "paylike.com", "paypa1.com", "paypa1.org", "paypaal.com", "paypal-activate.com", "paypal-activate.info", "paypal-activate.org", "paypal-apac.com", "paypal-apps.com", "paypal-biz.com", "paypal-brandcentral.com", "paypal-business.com", "paypal-business.net", "paypal-business.org", "paypal-cardcash.com", "paypal-cash.com", "paypal-center.com", "paypal-center.info", "paypal-center.net", "paypal-center.org", "paypal-communication.com", "paypal-communications.com", "paypal-communications.net", "paypal-community.com", "paypal-community.net", "paypal-comunidad.com", "paypal-corp.com", "paypal-database.com", "paypal-database.us", "paypal-donations.com", "paypal-dynamic.com", "paypal-engineering.com", "paypal-europe.com", "paypal-excelinvoicing.com", "paypal-exchanges.com", "paypal-forward.com", "paypal-galactic.com", "paypal-gift.com", "paypal-gifts.com", "paypal-gpplus.com", "paypal-here.com", "paypal-hrsystem.com", "paypal-innovationlab.com", "paypal-integration.com", "paypal-japan.com", "paypal-knowledge.com", "paypal-labs.com", "paypal-latam.com", "paypal-learning.com", "paypal-login.com", "paypal-login.info", "paypal-login.org", "paypal-login.us", "paypal-luxury.com", "paypal-mainstreet.net", "paypal-marketing.com", "paypal-media.com", "paypal-merchantloyalty.com", "paypal-mobilemoney.com", "paypal-network.org", "paypal-notice.com", "paypal-notify.com", "paypal-online.info", "paypal-online.net", "paypal-online.org", "paypal-optimizer.com", "paypal-pages.com", "paypal-photocard.com", "paypal-plaza.com", "paypal-portal.com", "paypal-prepagata.com", "paypal-prepagata.net", "paypal-prepaid.com", "paypal-profile.com", "paypal-proserv.com", "paypal-qrshopping.org", "paypal-recargacelular.com", "paypal-redeem.com", "paypal-referral.com", "paypal-retail.com", "paypal-scoop.com", "paypal-search.com", "paypal-secure.net", "paypal-secure.org", "paypal-security.net", "paypal-security.org", "paypal-service.org", "paypal-signin.com", "paypal-signin.us", "paypal-special.com", "paypal-specialoffers.com", "paypal-sptam.com", "paypal-status.com", "paypal-support.com", "paypal-survey.com", "paypal-survey.org", "paypal-team.com", "paypal-viewpoints.net", "paypal.ca", "paypal.com.cn", "paypal.com.hk", "paypal.com.sg", "paypal.hk", "paypal.info", "paypal.jp", "paypal.me", "paypal.net.cn", "paypal.org.cn", "paypal.so", "paypal.us", "paypalbeacon.com", "paypalbenefits.com", "paypalbrasil.com", "paypalcommunity.cn", "paypalcommunity.com", "paypalcommunity.net", "paypalcommunity.org", "paypalcorp.com", "paypalcredit.com", "paypalcreditcard.com", "paypalgivingfund.org", "paypalhere.cn", "paypalhere.com", "paypalhere.com.cn", "paypalhere.info", "paypalhere.net", "paypalhere.org", "paypalhere.tv", "paypali.net", "paypalinc.com", "paypalindia.com", "paypalinsuranceservices.org", "paypall.com", "paypallabs.com", "paypalme.com", "paypalnet.net", "paypalnet.org", "paypalnetwork.info", "paypalnetwork.net", "paypalnetwork.org", "paypalobjects.com", "paypalonline.net", "paypalonline.org", "paypalprepagata.com", "paypalprepagata.net", "paypalservice.com", "paypalshopping.com", "paypalshopping.net", "paypalsurvey.com", "paypalx.com", "paypaly.com", "payppal.com", "payypal.com", "pdncommunity.com", "pp-soc.com", "ppaypal.com", "pppds.com", "pypl.com", "pypl.info", "pypl.net", "pypl.tv", "s-xoom.com", "secure-paypal.info", "securepaypal.info", "simility.com", "sslpaypal.org", "swiftbank.info", "swiftbank.us", "swiftcapital.com", "swiftfinancial.com", "swiftfinancial.info", "swiftfinancial.net", "thepaypalshop.com", "theshoppingexpresslane.net", "venmo-touch.com", "venmo.info", "venmo.net", "venmo.org", "webmoneyinfo.com", "wiremoneytoirelandwithxoomeasierandcheaper.com", "www-paypal.info", "www-paypal.us", "wwwxoom.com", "xn--bnq297cix3a.cn", "xoom-experience.com", "xoom.com", "xoom.io", "xoom.net.cn", "xoom.us", "xoomcom.com"}, "paypal", "PayPal", "paypal.com", "paypal"},
	{[]string{"etsy.com"}, "etsy", "Etsy", "etsy.com", "etsy"},
	{[]string{"shopify.com", "myshopify.com", "shop.app", "shopify.dev", "shopifycdn.com", "shopifycloud.com", "shopifystatus.com", "shopifysvc.com"}, "shopify", "Shopify", "shopify.com", "shopify"},
	{[]string{"airbnb.com", "abb.travel", "accomable.com", "airbnb.ae", "airbnb.al", "airbnb.am", "airbnb.at", "airbnb.az", "airbnb.ba", "airbnb.be", "airbnb.ca", "airbnb.cat", "airbnb.ch", "airbnb.cl", "airbnb.cn", "airbnb.co.cr", "airbnb.co.id", "airbnb.co.il", "airbnb.co.in", "airbnb.co.kr", "airbnb.co.nz", "airbnb.co.uk", "airbnb.co.ve", "airbnb.co.za", "airbnb.com.ar", "airbnb.com.au", "airbnb.com.bo", "airbnb.com.br", "airbnb.com.bz", "airbnb.com.co", "airbnb.com.ec", "airbnb.com.ee", "airbnb.com.gt", "airbnb.com.hk", "airbnb.com.hn", "airbnb.com.hr", "airbnb.com.kh", "airbnb.com.mt", "airbnb.com.my", "airbnb.com.ni", "airbnb.com.pa", "airbnb.com.pe", "airbnb.com.ph", "airbnb.com.py", "airbnb.com.ro", "airbnb.com.sg", "airbnb.com.sv", "airbnb.com.tr", "airbnb.com.tw", "airbnb.com.ua", "airbnb.com.vn", "airbnb.cz", "airbnb.de", "airbnb.dk", "airbnb.es", "airbnb.fi", "airbnb.fr", "airbnb.gr", "airbnb.gy", "airbnb.hu", "airbnb.ie", "airbnb.is", "airbnb.it", "airbnb.jp", "airbnb.la", "airbnb.lt", "airbnb.lu", "airbnb.lv", "airbnb.me", "airbnb.mx", "airbnb.nl", "airbnb.no", "airbnb.org", "airbnb.pl", "airbnb.pt", "airbnb.rs", "airbnb.ru", "airbnb.se", "airbnb.si", "airbnb.tools", "airbnb.travel", "muscache.com", "vamo.com"}, "airbnb", "Airbnb", "airbnb.com", "airbnb"},
	{[]string{"booking.com", "booking.cn", "bstatic.com"}, "booking", "Booking.com", "booking.com", "booking"},
	{[]string{"agoda.com"}, "agoda", "Agoda", "agoda.com", "agoda"},
	{[]string{"expedia.com"}, "expedia", "Expedia", "expedia.com", "expedia"},
	{[]string{"tripadvisor.com"}, "tripadvisor", "Tripadvisor", "tripadvisor.com", "tripadvisor"},
	{[]string{"uber.com", "uber-assets.com", "ubereats.com"}, "uber", "Uber", "uber.com", "uber"},
	{[]string{"lyft.com"}, "lyft", "Lyft", "lyft.com", "lyft"},
	{[]string{"doordash.com"}, "doordash", "DoorDash", "doordash.com", "doordash"},
	{[]string{"grubhub.com"}, "grubhub", "Grubhub", "grubhub.com", "grubhub"},
	{[]string{"deliveroo.com"}, "deliveroo", "Deliveroo", "deliveroo.com", "deliveroo"},
	{[]string{"walmart.com", "w-mt.co", "wal-mart.com", "wal.co", "walmart-content.com", "walmart.cn", "walmart.pharmacy", "walmartimages.com", "wmt.co"}, "walmart", "Walmart", "walmart.com", "walmart"},
	{[]string{"target.com", "targetimg1.com"}, "target", "Target", "target.com", "target"},
	{[]string{"bestbuy.com", "bby.com", "bbycastatic.ca", "bbycloud.com", "bbycontent.com", "bbycontent.net", "bbyintl.com", "bbystatic.com", "bbyurl.us", "bestbhy.com", "bestbuy-audio.com", "bestbuy-communityrelations.com", "bestbuy-giftcard.info", "bestbuy-int.com", "bestbuy-jobs.com", "bestbuy.ca", "bestbuy.cn", "bestbuy.com.cn", "bestbuy.com.mx", "bestbuy.info", "bestbuy24x7solutions.com", "bestbuybusiness.com", "bestbuybusinessadvantageaccount.com", "bestbuycanada.ca", "bestbuycanada.com", "bestbuycanadaltd.ca", "bestbuycharityclassic.com", "bestbuycoffeemakers.com", "bestbuyethics.com", "bestbuyforbusiness.ca", "bestbuygsm.com", "bestbuyideax.com", "bestbuyphotoworkshoptours.com", "bestbuyrewards.com", "bestbuyrewardzone.ca", "bestbuys.com", "bestbuysgeeksquad.com", "bestbuysolutions.net", "bestbuystores.com", "bestbuytradein.com", "bestbuyus.com", "besztbuy.com", "bextbuy.com", "bienvenuechezbestbuy.ca", "cowboom.com", "dealtree.org", "futureshop.ca", "geek-squad-support.com", "geek-squad.net", "geek-squad.org", "geek-squads.net", "geeksquad.ca", "geeksquad.cc", "geeksquad.com", "geeksquad.net", "geeksquad.tv", "geeksquadcares.com", "geeksquadcentral.com", "geeksquadforums.com", "geeksquadonline.com", "geeksquadprotectionplan.org", "geeksquadservices.org", "geeksquadwebroot.org", "gscanada.info", "gsccdn.com", "gslink.us", "initproducts.com", "magnoliaav.com.cn", "mybestbuy.com", "mybestbuyclaims.com", "myrewardzone.com", "myrz.com", "onlinegeeksquad.com", "pacifickitchenandhome.com", "rocketfishproducts.com", "techliquidators.com", "tiendabestbuy.com", "unbrandedproducts.com", "welcometobestbuy.ca", "wolfatbestbuy.com", "wolfatbestbuy.net", "www-bestbuystores.com"}, "bestbuy", "Best Buy", "bestbuy.com", "bestbuy"},
	{[]string{"wish.com"}, "wish", "Wish", "wish.com", "wish"},
	{[]string{"costco.com", "costco-static.com", "costcobusinessdelivery.com"}, "costco", "Costco", "costco.com", "costco"},
	{[]string{"ikea.com", "ikea.ae", "ikea.at", "ikea.au", "ikea.be", "ikea.bg", "ikea.bh", "ikea.ca", "ikea.ch", "ikea.cl", "ikea.cn", "ikea.co.at", "ikea.co.ca", "ikea.co.de", "ikea.co.id", "ikea.co.il", "ikea.co.jp", "ikea.co.kr", "ikea.co.om", "ikea.co.ph", "ikea.co.pl", "ikea.co.th", "ikea.co.uk", "ikea.com.au", "ikea.com.cy", "ikea.com.de", "ikea.com.do", "ikea.com.eg", "ikea.com.gr", "ikea.com.hk", "ikea.com.in", "ikea.com.kw", "ikea.com.lv", "ikea.com.mx", "ikea.com.my", "ikea.com.om", "ikea.com.pl", "ikea.com.pr", "ikea.com.pt", "ikea.com.ro", "ikea.com.ru", "ikea.com.sa", "ikea.com.sg", "ikea.com.tr", "ikea.com.tw", "ikea.com.ua", "ikea.cz", "ikea.de", "ikea.dk", "ikea.do", "ikea.ee", "ikea.eg", "ikea.es", "ikea.fi", "ikea.fr", "ikea.gr", "ikea.hk", "ikea.hr", "ikea.hu", "ikea.ie", "ikea.in", "ikea.is", "ikea.it", "ikea.jo", "ikea.jp", "ikea.kr", "ikea.lt", "ikea.lv", "ikea.ma", "ikea.mx", "ikea.my", "ikea.net", "ikea.nl", "ikea.no", "ikea.om", "ikea.ph", "ikea.pl", "ikea.pr", "ikea.pt", "ikea.qa", "ikea.ro", "ikea.rs", "ikea.ru", "ikea.sa", "ikea.sg", "ikea.si", "ikea.sk", "ikea.ua", "ikea.us", "ingka-dt.cn", "ingka-internal.cn", "ingka-system.cn", "ingka.com", "ingka.dev", "ingkacentres.com"}, "ikea", "IKEA", "ikea.com", "ikea"},
	{[]string{"nike.com", "air-nike-shoes.com", "airmax360.com", "beijingnike.com", "betternike.com", "buynikechina.com", "cheap-nike.com", "cheapnikedunks.com", "cheapnikeoutlet.com", "classicnike.com", "cnnikebrand.com", "customnikeshoes.com", "easynike.com", "eenike.com", "fashionnike.com", "finenike.com", "foampositeshoes.com", "goldnikeclub.com", "gonike.com", "handbags-nike.com", "hightopnikes.com", "idnike.com", "inikesneakers.com", "just-do-it.cn", "justdoit.us", "kicksnike1.com", "kidsnikeshoes.com", "mynike.com", "nike-air-max.com", "nike-dunksb.com", "nike-fr.com", "nike-org.com", "nike-shox.cn", "nike-uk.com", "nike-us.com", "nike-usa.com", "nike.ci", "nike.cn", "nike.com.br", "nike.com.hk", "nike.gy", "nike.hk", "nike.host", "nike.shop", "nike.us", "nike.xn--hxt814e", "nike0594.com", "nike23.com", "nikeadidas.com", "nikeairhuarache.com", "nikeairmax.com", "nikeairmaxs.com", "nikeairmaxshoes.com", "nikeairrift.com", "nikeasia.com", "nikeaustralia.com", "nikeaw77.com", "nikeb.com", "nikebbn.com", "nikebetrue.com", "nikebetterworld.cn", "nikebetterworld.com", "nikebetterworld.com.cn", "nikebetterworld.info", "nikebetterworld.net", "nikebetterworld.org", "nikebiz.info", "nikebuy.com", "nikeby.com", "nikecdn.com", "nikechina.cn", "nikechosen.com", "nikecloud.com", "nikeclub.com", "nikecompany.com", "nikeconfluence.com", "nikecraft.com", "nikedawn.com", "nikedunks.net", "nikedunksshoes.com", "nikeelite.com", "nikefactorystore.com", "nikefind.com", "nikefirm.com", "nikefoamposites.com", "nikefoampositeshoes.com", "nikefootballcleats.com", "nikefootballgloves.com", "nikefootballjersey.com", "nikefoundation.org", "nikefr.com", "nikefrance.com", "nikefree.com", "nikefreeshoes.com", "nikefuelband.com", "nikegadgets.com", "nikegenealogy.com", "nikegolf.ca", "nikegolf.cn", "nikegrid.com", "nikehelp.com", "nikehightops.com", "nikehyperdunk.com", "nikeid.cn", "nikeid.com.cn", "nikeidshoes.com", "nikeinc.com", "nikeinc.com.cn", "nikeincchemistry.com", "nikeinstock.com", "nikeit.com", "nikeitalia.com", "nikeitaly.com", "nikekd.com", "nikekid.com", "nikelab.cn", "nikelab.com.cn", "nikelink.com", "nikeliuxiang.cn", "nikeliuxiang.com", "nikeliuxiang.com.cn", "nikelives.com", "nikelunarglide.com", "nikemall.cn", "nikemall.com.cn", "nikemarketing.com", "niken7.com", "nikenews.com", "nikeonlinestore.com", "nikeoutletstore.com", "nikeoutletstores.com", "nikepioneer.com", "nikeplus.com", "nikeplus.org", "nikepress.com", "nikeprice.com", "nikeproduct.com", "nikepromax.com", "nikeresponsibility.com", "nikereuseashoe.com", "nikerunclub.cn", "nikerunclub.com.cn", "nikerunner.com", "nikerunning.com.cn", "nikerunningshoes.com", "nikesb.com", "nikesbdunks.net", "nikeseason.com", "nikeselling.com", "nikesellorder.com", "nikeshoemarket.com", "nikeshoes-store.com", "nikeshoes21.com", "nikeshoes4u.com", "nikeshoescity.com", "nikeshoesgroup.com", "nikeshoesinc.com", "nikeshoesmarket.com", "nikeshoesretro.com", "nikeshoeswon.com", "nikeshoponline.com", "nikeshoxsale.com", "nikesku.com", "nikesnowboarding.com", "nikesoccercleats.com", "nikesportswear.com", "nikestares.com", "nikestore.cn", "nikestore.com", "nikestore.com.au", "nikestyles.com", "nikeswim.com", "niketaiwan.net", "niketracking.com", "niketradeweb.com", "niketrainer.com", "niketrainers.com", "niketraining.com", "nikeusa.com", "nikewear.com", "nikey.com", "nikezoom.com", "nke6.com", "outletnike.com", "runnike.com", "runningnike.com", "sale-nikeshoes.com", "static-nike.com", "swoo.sh", "swoosh.com", "swoosh.tv", "thedailysnkr.com", "verygoodnike.com", "womensnikeshox.com", "xn--74q035i.xn--hxt814e"}, "nike", "Nike", "nike.com", "nike"},
	{[]string{"adidas.com", "adidas-archive.org", "adidas-group.ca", "adidas-group.com", "adidas.ae", "adidas.at", "adidas.be", "adidas.bh", "adidas.ca", "adidas.ch", "adidas.cl", "adidas.cn", "adidas.co", "adidas.co.id", "adidas.co.in", "adidas.co.kr", "adidas.co.nz", "adidas.co.th", "adidas.co.uk", "adidas.co.za", "adidas.com.ar", "adidas.com.au", "adidas.com.br", "adidas.com.cn", "adidas.com.co", "adidas.com.eg", "adidas.com.hk", "adidas.com.my", "adidas.com.pe", "adidas.com.ph", "adidas.com.qa", "adidas.com.sa", "adidas.com.sg", "adidas.com.tr", "adidas.com.tw", "adidas.com.vn", "adidas.cz", "adidas.de", "adidas.dk", "adidas.es", "adidas.fi", "adidas.fr", "adidas.gr", "adidas.hu", "adidas.ie", "adidas.in", "adidas.it", "adidas.jp", "adidas.mx", "adidas.nl", "adidas.no", "adidas.pe", "adidas.pl", "adidas.pt", "adidas.ru", "adidas.se", "adidas.sk", "adidasadmin.com", "adidasarchive.org", "adidasbadminton.com", "adidasboxing.com", "adidascdn.com", "adidasgymnastics.com", "adidashockey.com", "adidasmotorsport.com", "adidassailing.com", "adidasspecialtysports.at", "adidasspecialtysports.be", "adidasspecialtysports.cn", "adidasspecialtysports.co.uk", "adidasspecialtysports.cz", "adidasspecialtysports.de", "adidasspecialtysports.dk", "adidasspecialtysports.es", "adidasspecialtysports.fi", "adidasspecialtysports.fr", "adidasspecialtysports.ie", "adidasspecialtysports.it", "adidasspecialtysports.nl", "adidasspecialtysports.pl", "adidasspecialtysports.ru", "adidasspecialtysports.se", "adidasspecialtysports.sk", "adidastabletennis.com", "adidasusnews.com", "miadidas.com", "micoach.com", "miteam.com", "shopadidas.ca", "shopadidas.com"}, "adidas", "Adidas", "adidas.com", "adidas"},
	{[]string{"sephora.com"}, "sephora", "Sephora", "sephora.com", "sephora"},
	{[]string{"zalando.com"}, "zalando", "Zalando", "zalando.com", "zalando"},
	{[]string{"amazonaws.com", "a2z.org.cn", "acmvalidations.com", "acmvalidationsaws.com", "aesworkshops.com", "amazonaws-china.com", "amazonaws.biz", "amazonaws.cn", "amazonaws.co.uk", "amazonaws.com.cn", "amazonaws.info", "amazonaws.net", "amazonaws.org", "amazonaws.tv", "amazoncognito.com", "amazonses.com", "amazonwebservices.cn", "amazonwebservices.com", "amazonwebservices.com.cn", "amazonworkdocs.cn", "amazonworkdocs.com", "amazonworkdocs.com.cn", "amplifyapp.com", "amplifyframework.com", "amzndns-cn.biz", "amzndns-cn.cn", "amzndns-cn.com", "amzndns-cn.net", "amzndns.co.uk", "amzndns.com", "amzndns.net", "amzndns.org", "asfiovnxocqpcry.com.cn", "aws-border.cn", "aws-icp-domain-manager.cn", "aws-iot-hackathon.com", "aws.a2z.com", "aws.com", "aws.dev", "awsapprunner.com", "awsapps.cn", "awsapps.com", "awsapps.com.cn", "awsautopilot.com", "awsautoscaling.com", "awsbraket.com", "awscommandlineinterface.com", "awsedstart.com", "awseducate.com", "awseducate.net", "awseducate.org", "awsglobalaccelerator.com", "awsloft-johannesburg.com", "awsloft-stockholm.com", "awssecworkshops.com", "awsstatic.cn", "awsstatic.com", "awsthinkbox.com", "awstrack.me", "awswaf.com", "cdkworkshop.com", "cloudfront-cn.net", "cloudfront-test.cn", "cloudfront.cn", "cloudfront.com", "containersonaws.com", "elasticbeanstalk.com", "nwcdcloud.cn", "nwcdcloud.com.cn", "nwcddns.cn", "nwcdinfosec.cn", "route53.cn", "sagemaker.com.cn", "thinkboxsoftware.com"}, "aws", "AWS", "amazonaws.com", "aws"},
	{[]string{"cnn.com", "cnn.io", "cnn.it", "cnnarabic.com", "cnnlabs.com", "cnnmoney.ch", "cnnmoney.com", "cnnmoneystream.com", "cnnpolitics.com"}, "cnn", "CNN", "cnn.com", "cnn"},
	{[]string{"nytimes.com", "nyt.com", "nyt.net", "nytchina.com", "nytcn.me", "nytco.com", "nyti.ms", "nytstyle.com", "timestalks.com"}, "nytimes", "The New York Times", "nytimes.com", "nytimes"},
	{[]string{"theguardian.com", "dev-guardianapis.com", "dev-theguardian.com", "gu-web.net", "guardianapis.com", "guardianapps.co.uk", "guim.co.uk", "guimcode.co.uk", "theguardiandns.com"}, "theguardian", "The Guardian", "theguardian.com", "theguardian"},
	{[]string{"reuters.com", "reut.rs", "reuters.tv", "reutersmedia.net"}, "reuters", "Reuters", "reuters.com", "reuters"},
	{[]string{"forbes.com", "forbesimg.com"}, "forbes", "Forbes", "forbes.com", "forbes"},
	{[]string{"washingtonpost.com"}, "washingtonpost", "The Washington Post", "washingtonpost.com", "washingtonpost"},
	{[]string{"wsj.com", "careerjournal.com", "collegejournal.com", "getwsj.com", "opinionjournal.com", "realestatejournal.com", "startupjournal.com", "wsj.jobs", "wsj.net", "wsjmediakit.com", "wsjplus.com", "wsjshop.com", "wsjwine.com"}, "wsj", "The Wall Street Journal", "wsj.com", "wsj"},
	{[]string{"economist.com", "economistgroup.com", "economistgroupcareers.com", "eiu.com"}, "economist", "The Economist", "economist.com", "economist"},
	{[]string{"time.com"}, "time", "TIME", "time.com", "time"},
	{[]string{"aljazeera.com", "ajplus.net", "aljazeera.net", "palestineremix.com", "sarajevopodopsadom.com", "srebrenica360.com", "syhacked.com"}, "aljazeera", "Al Jazeera", "aljazeera.com", "aljazeera"},
	{[]string{"dw.com", "deutschewelle.h-cdn.com"}, "dw", "DW", "dw.com", "dw"},
	{[]string{"france24.com"}, "france24", "France 24", "france24.com", "france24"},
	{[]string{"axios.com"}, "axios", "Axios", "axios.com", "axios"},
	{[]string{"politico.com"}, "politico", "Politico", "politico.com", "politico"},
	{[]string{"huffpost.com", "huffingtonpost.ca", "huffingtonpost.co.uk", "huffingtonpost.co.za", "huffingtonpost.com", "huffingtonpost.com.au", "huffingtonpost.com.mx", "huffingtonpost.de", "huffingtonpost.es", "huffingtonpost.fr", "huffingtonpost.gr", "huffingtonpost.in", "huffingtonpost.it", "huffingtonpost.jp", "huffingtonpost.kr", "huffpostarabi.com", "huffpostbrasil.com", "huffpostmaghreb.com"}, "huffpost", "HuffPost", "huffpost.com", "huffpost"},
	{[]string{"buzzfeed.com"}, "buzzfeed", "BuzzFeed", "buzzfeed.com", "buzzfeed"},
	{[]string{"vice.com"}, "vice", "Vice", "vice.com", "vice"},
	{[]string{"wired.com"}, "wired", "Wired", "wired.com", "wired"},
	{[]string{"theverge.com"}, "verge", "The Verge", "theverge.com", "verge"},
	{[]string{"techcrunch.com"}, "techcrunch", "TechCrunch", "techcrunch.com", "techcrunch"},
	{[]string{"engadget.com"}, "engadget", "Engadget", "engadget.com", "engadget"},
	{[]string{"gizmodo.com"}, "gizmodo", "Gizmodo", "gizmodo.com", "gizmodo"},
	{[]string{"cnet.com", "cnet.co.kr", "cnet.de", "cnetfrance.fr", "download.com"}, "cnet", "CNET", "cnet.com", "cnet"},
	{[]string{"bbc.com", "bbc.co.uk", "bbc-reporting-api.app", "bbc.in", "bbc.mp-pxcdn.com", "bbc.net.uk", "bbcfmt.s.llnwi.net", "bbci.co.uk", "bbcmedia.co.uk", "bbcpersian.com", "bbcverticals.com", "bidi.net.uk"}, "bbc", "BBC", "bbc.com", "bbc"},
	{[]string{"bloomberg.com", "bbgevent.com", "bbhub.io", "bbthat.com", "bgov.com", "bloomberg.cn", "bloomberg.co.jp", "bloomberg.co.kr", "bloomberg.com.br", "bloomberg.fm", "bloomberg.tv", "bloombergapps.com", "bloombergarcade.com", "bloombergbeta.com", "bloombergbna.com", "bloombergbreakaway.com", "bloombergbriefs.com", "bloombergchina.com", "bloombergenvironment.com", "bloombergforeducation.com", "bloomberggovernment.com", "bloombergindices.com", "bloombergindustry.com", "bloomberglabs.com", "bloomberglaw.com", "bloomberglive.com", "bloomberglp.com", "bloombergmedia.com", "bloombergnext.com", "bloombergpolarlake.com", "bloombergprep.com", "bloombergquint.com", "bloombergradio.com", "bloombergsef.com", "bloombergspace.com", "bloombergstatus.com", "bloombergsurvey.com", "bloombergtax.com", "bloombergtax1.com", "bloombergtaxtech.com", "bloombergtradingchallenge.com", "bloombergtv.mn", "bloombergvault.com", "bloombergview.com", "blpcareers.com", "blpevents.com", "blpprofessional.com", "bmia.org", "bnef.com", "businessweek.com", "businessweekmag.com", "bwbx.io", "citylab.com", "gotraffic.net", "londonmithraeum.com", "neweconomyforum.com", "newenergyfinance.com", "quicktake.video", "techatbloomberg.com", "tictoc.video"}, "bloomberg", "Bloomberg", "bloomberg.com", "bloomberg"},
	{[]string{"codepen.io"}, "codepen", "CodePen", "codepen.io", "codepen"},
	{[]string{"replit.com"}, "replit", "Replit", "replit.com", "replit"},
	{[]string{"vercel.com", "ai-sdk.dev", "err.sh", "hyper.is", "nextjs.org", "now.sh", "skills.sh", "static.fun", "title.sh", "turborepo.org", "vercel-dns.com", "vercel-status.com", "vercel.blog", "vercel.dev", "vercel.events", "vercel.live", "vercel.pub", "vercel.sh", "vercel.store", "zeit-world.co.uk", "zeit-world.com", "zeit-world.net", "zeit-world.org", "zeit.co", "zeit.sh", "zeitworld.com"}, "vercel", "Vercel", "vercel.com", "vercel"},
	{[]string{"netlify.com"}, "netlify", "Netlify", "netlify.com", "netlify"},
	{[]string{"heroku.com"}, "heroku", "Heroku", "heroku.com", "heroku"},
	{[]string{"digitalocean.com", "digitalocean-ui.com", "digitaloceanspaces.com", "do.co", "nginxconfig.io", "ondigitalocean.com"}, "digitalocean", "DigitalOcean", "digitalocean.com", "digitalocean"},
	{[]string{"linode.com"}, "linode", "Linode", "linode.com", "linode"},
	{[]string{"vultr.com"}, "vultr", "Vultr", "vultr.com", "vultr"},
	{[]string{"cloudflare.com", "argotunnel.com", "browser.run", "cf-china.info", "cf-ipfs.com", "cf-ns.com", "cf-ns.net", "cf-ns.site", "cf-ns.tech", "cfargotunnel.com", "cfdata.org", "cfl.re", "cftest5.cn", "cftest6.cn", "cftest7.com", "cftest8.com", "cloudflare-cn.com", "cloudflare-dns.com", "cloudflare-ech.com", "cloudflare-esni.com", "cloudflare-gateway.com", "cloudflare-ipfs.com", "cloudflare-quic.com", "cloudflare-terms-of-service-abuse.com", "cloudflare.app", "cloudflare.dev", "cloudflare.net", "cloudflare.pay", "cloudflare.tv", "cloudflareaccess.com", "cloudflareanycast.net", "cloudflareapps.com", "cloudflarebolt.com", "cloudflarebrowser.com", "cloudflarechallenge.com", "cloudflarechina.cn", "cloudflareclient.com", "cloudflarecn.net", "cloudflarecp.com", "cloudflareglobal.net", "cloudflareinsights-cn.com", "cloudflareinsights.com", "cloudflareok.com", "cloudflarepartners.com", "cloudflareperf.com", "cloudflareportal.com", "cloudflarepreview.com", "cloudflareprod.com", "cloudflareregistrar.com", "cloudflareresearch.com", "cloudflareresolve.com", "cloudflaressl.com", "cloudflarestaging.com", "cloudflarestatus.com", "cloudflarestorage.com", "cloudflarestoragegw.com", "cloudflarestream.com", "cloudflaresupport.com", "cloudflaretest.com", "cloudflarewarp.com", "cloudflareworkers.com", "encryptedsni.com", "every1dns.net", "foundationdns.com", "foundationdns.net", "foundationdns.org", "imagedelivery.net", "isbgpsafeyet.com", "one.one.one", "pacloudflare.com", "r2.dev", "trycloudflare.com", "videodelivery.net", "warp.plus"}, "cloudflare", "Cloudflare", "cloudflare.com", "cloudflare"},
	{[]string{"npmjs.com"}, "npm", "npm", "npmjs.com", "npm"},
	{[]string{"docker.com", "compose-spec.io", "docker-images-prod.6aa30f8b08e16409b46e0173d6de2f56.r2.cloudflarestorage.com", "docker.io", "dockerstatic.com"}, "docker", "Docker", "docker.com", "docker"},
	{[]string{"kubernetes.io", "k8s.io"}, "kubernetes", "Kubernetes", "kubernetes.io", "kubernetes"},
	{[]string{"raspberrypi.com"}, "raspberrypi", "Raspberry Pi", "raspberrypi.com", "raspberrypi"},
	{[]string{"arduino.cc"}, "arduino", "Arduino", "arduino.cc", "arduino"},
	{[]string{"news.ycombinator.com", "startupschool.org", "ycombinator.com"}, "ycombinator", "Hacker News", "news.ycombinator.com", "ycombinator"},
	{[]string{"producthunt.com"}, "producthunt", "Product Hunt", "producthunt.com", "producthunt"},
	{[]string{"dev.to"}, "devdotto", "DEV", "dev.to", "devdotto"},
	{[]string{"hashnode.com"}, "hashnode", "Hashnode", "hashnode.com", "hashnode"},
	{[]string{"jsfiddle.net"}, "jsfiddle", "JSFiddle", "jsfiddle.net", "jsfiddle"},
	{[]string{"binance.com", "binance.cc", "binance.charity", "binance.cloud", "binance.co", "binance.info", "binance.me", "binance.net", "binance.org", "binance.us", "binance.vision", "binanceapi.com", "binancecnt.com", "binanceru.net", "binancezh.be", "binancezh.biz", "binancezh.cc", "binancezh.co", "binancezh.com", "binancezh.info", "binancezh.ink", "binancezh.kim", "binancezh.link", "binancezh.live", "binancezh.mobi", "binancezh.net", "binancezh.pro", "binancezh.sh", "binancezh.top", "bmwweb.solutions", "bnappweb.black", "bnbchain.org", "bnbstatic.com", "bntrace.com", "bsappapi.cc", "bsappapi.com", "bscdnweb.com", "nftstatic.com", "saasexch.cc", "saasexch.co", "saasexch.com", "saasexch.info", "saasexch.io", "zftksc.cdn-settings.appsflyersdk.com", "zftksc.launches.appsflyersdk.com"}, "binance", "Binance", "binance.com", "binance"},
	{[]string{"coinbase.com"}, "coinbase", "Coinbase", "coinbase.com", "coinbase"},
	{[]string{"kraken.com", "kraken.onl"}, "kraken", "Kraken", "kraken.com", "kraken"},
	{[]string{"okx.com", "okex.com", "oklink.com", "okx-dns.com", "okx-dns1.com", "okx-dns2.com", "okx.ac", "okx.cab", "okx.com.cdn.cloudflare.net", "xlayer.tech"}, "okx", "OKX", "okx.com", "okx"},
	{[]string{"bybit.com", "byabcde.com", "byapis.com", "byapps.net", "bybdc6.com", "bybit-global.com", "bybit.ada.support", "bybit.biz", "bybit.cloud", "bybitglobal.com", "bycbe.com", "bycsi.com", "byd3c3.com", "bymj.io", "bytick.com"}, "bybit", "Bybit", "bybit.com", "bybit"},
	{[]string{"bitstamp.net"}, "bitstamp", "Bitstamp", "bitstamp.net", "bitstamp"},
	{[]string{"metamask.io"}, "metamask", "MetaMask", "metamask.io", "metamask"},
	{[]string{"trustwallet.com"}, "trustwallet", "Trust Wallet", "trustwallet.com", "trustwallet"},
	{[]string{"ledger.com"}, "ledger", "Ledger", "ledger.com", "ledger"},
	{[]string{"trezor.io"}, "trezor", "Trezor", "trezor.io", "trezor"},
	{[]string{"uniswap.org"}, "uniswap", "Uniswap", "uniswap.org", "uniswap"},
	{[]string{"coingecko.com"}, "coingecko", "CoinGecko", "coingecko.com", "coingecko"},
	{[]string{"coinmarketcap.com"}, "coinmarketcap", "CoinMarketCap", "coinmarketcap.com", "coinmarketcap"},
	{[]string{"tradingview.com"}, "tradingview", "TradingView", "tradingview.com", "tradingview"},
	{[]string{"wise.com", "transferwise.com"}, "wise", "Wise", "wise.com", "wise"},
	{[]string{"revolut.com"}, "revolut", "Revolut", "revolut.com", "revolut"},
	{[]string{"stripe.com", "link.com", "stripe-terminal-local-reader.net", "stripe.dev", "stripe.events", "stripe.global", "stripe.network", "stripecdn.com"}, "stripe", "Stripe", "stripe.com", "stripe"},
	{[]string{"squareup.com"}, "square", "Square", "squareup.com", "square"},
	{[]string{"cash.app"}, "cashapp", "Cash App", "cash.app", "cashapp"},
	{[]string{"venmo.com"}, "venmo", "Venmo", "venmo.com", "venmo"},
	{[]string{"coursera.org", "coursera-for-business.org", "coursera.community", "coursera.help"}, "coursera", "Coursera", "coursera.org", "coursera"},
	{[]string{"udemy.com", "udemycdn.com"}, "udemy", "Udemy", "udemy.com", "udemy"},
	{[]string{"duolingo.com", "duolingo.cn"}, "duolingo", "Duolingo", "duolingo.com", "duolingo"},
	{[]string{"khanacademy.org", "kastatic.org"}, "khanacademy", "Khan Academy", "khanacademy.org", "khanacademy"},
	{[]string{"edx.org", "edx-cdn.org"}, "edx", "edX", "edx.org", "edx"},
	{[]string{"skillshare.com"}, "skillshare", "Skillshare", "skillshare.com", "skillshare"},
	{[]string{"masterclass.com"}, "masterclass", "MasterClass", "masterclass.com", "masterclass"},
	{[]string{"ted.com", "tedcdn.com"}, "ted", "TED", "ted.com", "ted"},
	{[]string{"imdb.com", "boxofficemojo.com", "imdb.to", "media-imdb.com"}, "imdb", "IMDb", "imdb.com", "imdb"},
	{[]string{"medium.com", "medium.systems"}, "medium", "Medium", "medium.com", "medium"},
	{[]string{"quora.com", "quoracdn.net", "poe.com", "poecdn.net", "qr.ae", "www.quora.com.cdn.cloudflare.net"}, "quora", "Quora", "quora.com", "quora"},
	{[]string{"fandom.com", "cortexrpg.com", "cursecdn.com", "dndbeyond.com", "fanatical.com", "fandom.zendesk.com", "futhead.com", "gamepedia.com", "muthead.com", "wikia.com", "wikia.nocookie.net", "wikia.org"}, "fandom", "Fandom", "fandom.com", "fandom"},
	{[]string{"dribbble.com", "drbl.in"}, "dribbble", "Dribbble", "dribbble.com", "dribbble"},
	{[]string{"behance.net"}, "behance", "Behance", "behance.net", "behance"},
	{[]string{"speedtest.net", "cdnst.net", "cellmaps.com", "ekahau.cloud", "ekahau.com", "ookla-speedtest-central.hgconair.hgc.com.hk", "ookla.com", "ooklaserver.net", "pingtest.net", "speedtest.app", "speedtest.cc", "speedtest.co", "speedtest.ee", "speedtest.in", "speedtest.me", "speedtest.ng", "speedtest.tv", "speedtestcustom.com", "webtest.net", "www.speedtest.net.cdn.cloudflare.net"}, "speedtest", "Speedtest", "speedtest.net", "speedtest"},
	{[]string{"letterboxd.com"}, "letterboxd", "Letterboxd", "letterboxd.com", "letterboxd"},
	{[]string{"trakt.tv"}, "trakt", "Trakt", "trakt.tv", "trakt"},
	{[]string{"goodreads.com", "gr-assets.com"}, "goodreads", "Goodreads", "goodreads.com", "goodreads"},
	{[]string{"wattpad.com"}, "wattpad", "Wattpad", "wattpad.com", "wattpad"},
	{[]string{"archive.org"}, "internetarchive", "Internet Archive", "archive.org", "internetarchive"},
	{[]string{"openstreetmap.org", "openmaps.org", "openstreetmap.com", "openstreetmap.net", "openstreetmaps.org", "osm.org", "osmfoundation.org", "stateofthemap.com", "stateofthemap.org"}, "openstreetmap", "OpenStreetMap", "openstreetmap.org", "openstreetmap"},
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

// siteSuffixMap 是后缀到服务的查表。后缀唯一性由 TestSiteSuffixesUnique 保证，
// 建表时不会互相覆盖。匹配时从 host 本身开始逐级剥掉首段标签、从长到短查表，
// 第一个命中就是最长后缀匹配，和原来按长度排序的线性扫描等价，表再大也不会变慢。
var siteSuffixMap map[string]*siteService

func init() {
	siteSuffixMap = make(map[string]*siteService)
	for i := range siteServices {
		for _, suffix := range siteServices[i].suffixes {
			siteSuffixMap[suffix] = &siteServices[i]
		}
	}
}

// baseDomain 把域名归约到可注册主域名（eTLD+1）：同主域名的各级子域名
// 在排行里归并到同一行。IP、单标签域名或算不出的，返回原域名。
func baseDomain(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	if base, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil && base != "" {
		return base
	}
	return host
}

// isOwnDomain 判断连接域名是不是本机域名：主机在安装时绑定的域名
// （Settings.Domain）所在的主域名家族都算本机流量，排行里排除——
// 绑定域名本身、它的子域名、同主域名下的其他子域名（同一域名下的
// 其他服务器）都排，因为整域名都是本机的。
func isOwnDomain(host, ownDomain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	ownDomain = strings.ToLower(strings.TrimSpace(ownDomain))
	ownDomain = strings.TrimSuffix(ownDomain, ".")
	if host == "" || ownDomain == "" {
		return false
	}
	return baseDomain(host) == baseDomain(ownDomain)
}

// mapSiteHost 把连接域名归到服务：返回服务 id；未映射的域名返回以主域名为
// 键的键，空域名返回未知服务键。
func mapSiteHost(host string) (key string, service *siteService) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "unknown", nil
	}
	for rest := host; ; {
		if service, ok := siteSuffixMap[rest]; ok {
			return service.id, service
		}
		i := strings.IndexByte(rest, '.')
		if i < 0 {
			break
		}
		rest = rest[i+1:]
	}
	return "domain:" + baseDomain(host), nil
}

// serviceIPRanges 是各服务商公开的自有网络 IP 段：只有域名为空的直连 IP
// 连接才按 IP 段归类（域名优先，IP 只补域名不存在的情况）。只收自有网络、
// 不会混进第三方流量的段；云厂商共享段（AWS、Azure、谷歌云、Cloudflare
// 等）故意不收，否则第三方站点会被错归到云厂商名下。
var serviceIPRanges = map[string][]string{
	"telegram": {
		"149.154.164.0/22", "149.154.168.0/22", "149.154.172.0/22",
		"91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22",
		"91.105.192.0/24",
		"2001:b28:f23d::/48", "2001:b28:f23f::/48", "2001:67c:4e8::/48",
	},
	"apple": {"17.0.0.0/8"},
}

// serviceIPRangeOrder 固定 IP 段的匹配顺序（各服务段互不重叠，顺序只为确定性）。
var serviceIPRangeOrder = []string{"telegram", "apple"}

var serviceIPNets = func() map[string][]netip.Prefix {
	nets := make(map[string][]netip.Prefix, len(serviceIPRanges))
	for id, ranges := range serviceIPRanges {
		for _, r := range ranges {
			if prefix, err := netip.ParsePrefix(r); err == nil {
				nets[id] = append(nets[id], prefix)
			}
		}
	}
	return nets
}()

// mapSiteIP 按目标 IP 段归到服务；不在任何段里返回 nil。
func mapSiteIP(dst string) *siteService {
	addr, err := netip.ParseAddr(strings.TrimSpace(dst))
	if err != nil || !addr.IsValid() {
		return nil
	}
	addr = addr.Unmap()
	for _, id := range serviceIPRangeOrder {
		for _, prefix := range serviceIPNets[id] {
			if prefix.Contains(addr) {
				return siteServiceByID(id)
			}
		}
	}
	return nil
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
func aggregateSiteRecords(dataDir, relayUsername, ownDomain string, force bool) {
	siteAggregateMu.Lock()
	defer siteAggregateMu.Unlock()
	now := time.Now()
	if !force && now.Sub(siteLastAggregate) < siteAggregateMinGap {
		return
	}
	siteLastAggregate = now
	if err := aggregateSiteRecordsOnce(dataDir, relayUsername, ownDomain, now); err != nil {
		log.Printf("聚合网站排行账本失败：%v", err)
	}
}

func aggregateSiteRecordsOnce(dataDir, relayUsername, ownDomain string, now time.Time) error {
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
		if isOwnDomain(record.Host, ownDomain) {
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
			if record.Host == "" {
				if service := mapSiteIP(record.Dst); service != nil {
					key = service.id
				}
			}
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
func buildSiteRanking(ledger *daySiteLedger, username, ownDomain string) siteRanking {
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
	// 先按展示键归并：旧账本里按完整域名存的行，按当前服务表重归并，
	// 同主域名的子域名、以及新旧两种键，合并成同一行。
	// 未知域名（空主机）不成行，直接计入其余合计；本机域名行同样排除。
	merged := map[string]*siteTotals{}
	otherCount, otherBytes := 0, int64(0)
	for key, totals := range userServices {
		if totals == nil || totals.bytes() == 0 {
			continue
		}
		if key == "unknown" {
			otherCount++
			otherBytes += totals.bytes()
			continue
		}
		displayKey := key
		if host, ok := strings.CutPrefix(key, "domain:"); ok {
			if isOwnDomain(host, ownDomain) {
				continue
			}
			displayKey, _ = mapSiteHost(baseDomain(host))
		}
		m := merged[displayKey]
		if m == nil {
			m = &siteTotals{}
			merged[displayKey] = m
		}
		m.Upload += totals.Upload
		m.Download += totals.Download
		m.Duration += totals.Duration
	}
	rows := make([]scored, 0, len(merged))
	for key, totals := range merged {
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
	ranking.OtherCount += otherCount
	ranking.OtherBytes += otherBytes
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
				Host          string `json:"host"`
				DestinationIP string `json:"destinationIP"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		return icons
	}
	seen := map[string]bool{}
	for _, connection := range snapshot.Connections {
		host := strings.TrimSpace(connection.Metadata.Host)
		var service *siteService
		if host != "" {
			_, service = mapSiteHost(host)
		} else {
			// 域名为空的直连 IP 连接：按 IP 段认，认不出照旧跳过。
			service = mapSiteIP(connection.Metadata.DestinationIP)
			if service == nil {
				continue
			}
		}
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
