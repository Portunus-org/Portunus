import type { Locale } from "@/lib/messages"

/**
 * Ready-made sets a routing rule can use, downloaded as sing-box rule-sets
 * and refreshed daily by the panel. A snapshot of what the projects publish;
 * any other set works through "From a link".
 *
 * Addresses by country: SagerNet/sing-geoip. Sites by service or category:
 * SagerNet/sing-geosite. Russia lists: itdoginfo/allow-domains.
 */
export interface CatalogItem {
  id: string
  name: Record<Locale, string>
  url: string
  group: "lists" | "services" | "countries"
}

const GEOIP = "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-"
const GEOSITE = "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-"
const ITDOG = "https://raw.githubusercontent.com/itdoginfo/allow-domains/main/"

const lists: CatalogItem[] = [
  { id: "ru-inside", group: "lists", url: ITDOG + "Russia/inside-raw.lst", name: { en: "Sites blocked in Russia", ru: "Сайты, заблокированные в России" } },
  { id: "ru-outside", group: "lists", url: ITDOG + "Russia/outside-raw.lst", name: { en: "Russian sites closed to visitors from abroad", ru: "Российские сайты, закрытые для иностранных адресов" } },
  { id: "site-category-ru", group: "lists", url: GEOSITE + "category-ru.srs", name: { en: "Russian sites", ru: "Российские сайты" } },
  { id: "site-category-gov-ru", group: "lists", url: GEOSITE + "category-gov-ru.srs", name: { en: "Russian government sites", ru: "Российские государственные сайты" } },
  { id: "site-cn", group: "lists", url: GEOSITE + "cn.srs", name: { en: "Chinese sites", ru: "Китайские сайты" } },
  { id: "site-category-ads-all", group: "lists", url: GEOSITE + "category-ads-all.srs", name: { en: "Advertising and trackers", ru: "Реклама и трекеры" } },
]

const services: CatalogItem[] = (
  [
    ["youtube", "YouTube"],
    ["google", "Google"],
    ["telegram", "Telegram"],
    ["whatsapp", "WhatsApp"],
    ["meta", "Meta"],
    ["instagram", "Instagram"],
    ["facebook", "Facebook"],
    ["x", "X (Twitter)"],
    ["tiktok", "TikTok"],
    ["discord", "Discord"],
    ["netflix", "Netflix"],
    ["spotify", "Spotify"],
    ["twitch", "Twitch"],
    ["openai", "OpenAI"],
    ["category-ai-!cn", "AI services"],
    ["github", "GitHub"],
    ["microsoft", "Microsoft"],
    ["apple", "Apple"],
    ["amazon", "Amazon"],
    ["cloudflare", "Cloudflare"],
    ["steam", "Steam"],
    ["linkedin", "LinkedIn"],
    ["reddit", "Reddit"],
    ["signal", "Signal"],
    ["zoom", "Zoom"],
    ["wikimedia", "Wikipedia"],
    ["speedtest", "Speedtest"],
    ["yandex", "Yandex"],
    ["vk", "VK"],
  ] as const
).map(([slug, name]) => ({ id: "site-" + slug, group: "services", url: GEOSITE + slug + ".srs", name: { en: name, ru: name === "AI services" ? "Сервисы ИИ" : name === "Wikipedia" ? "Википедия" : name === "Yandex" ? "Яндекс" : name === "VK" ? "ВКонтакте" : name } }))

// Country codes with a published address set.
const countryCodes = "ad ae af ag ai al am ao aq ar as at au aw ax az ba bb bd be bf bg bh bi bj bl bm bn bo bq br bs bt bw by bz ca cd cf cg ch ci ck cl cm cn co cr cu cv cw cy cz de dj dk dm do dz ec ee eg er es et fi fj fk fm fo fr ga gb gd ge gf gg gh gi gl gm gn gp gq gr gt gu gw gy hk hn hr ht hu id ie il im in io iq ir is it je jm jo jp ke kg kh ki km kn kp kr kw ky kz la lb lc li lk lr ls lt lu lv ly ma mc md me mf mg mh mk ml mm mn mo mp mq mr ms mt mu mv mw mx my mz na nc ne nf ng ni nl no np nr nu nz om pa pe pf pg ph pk pl pm pr ps pt pw py qa re ro rs ru rw sa sb sc sd se sg si sk sl sm sn so sr ss st sv sx sy sz tc td tg th tj tk tl tm tn to tr tt tv tw tz ua ug us uy uz va vc ve vg vi vn vu wf ws ye yt za zm zw".split(" ")

/** The whole catalog, with country names in the given language. */
export function routeCatalog(locale: Locale): CatalogItem[] {
  let names: Intl.DisplayNames | null = null
  try {
    names = new Intl.DisplayNames([locale], { type: "region" })
  } catch {
    // Very old browsers: fall back to the codes.
  }
  const countries: CatalogItem[] = countryCodes
    .map((cc) => {
      const name = names?.of(cc.toUpperCase()) ?? cc.toUpperCase()
      return { id: "ip-" + cc, group: "countries" as const, url: GEOIP + cc + ".srs", name: { en: name, ru: name } }
    })
    .sort((a, b) => a.name[locale].localeCompare(b.name[locale], locale))
  return [...lists, ...services, ...countries]
}
