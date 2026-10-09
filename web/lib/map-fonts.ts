import {
  Noto_Sans_Arabic,
  Noto_Sans_Hebrew,
  Noto_Sans_Armenian,
  Noto_Sans_Georgian,
  Noto_Sans_Devanagari,
  Noto_Sans_Bengali,
  Noto_Sans_Thai,
  Noto_Sans_Lao,
  Noto_Sans_Khmer,
  Noto_Sans_Myanmar,
  Noto_Sans_Ethiopic,
  Noto_Sans_Sinhala,
  Noto_Sans_Tamil,
} from "next/font/google"

/**
 * Fonts for the scripts names on the map are written in, beyond the Latin,
 * Cyrillic and Greek of the panel's own typeface. Many computers have no
 * font for Burmese or Khmer, say, and would show rows of boxes; with these
 * the map does not depend on what is installed. They are served by the
 * panel like the rest of the interface, each in pieces by range of letters,
 * and a browser fetches a piece only when a name on screen needs it.
 * Chinese, Japanese and Korean are left to the system: their fonts are tens
 * of megabytes, and systems that show those languages have them.
 */
// (The options are spelt out in every call: the build reads them as text.)
const arabic = Noto_Sans_Arabic({ display: "swap", preload: false, subsets: ["arabic"] })
const hebrew = Noto_Sans_Hebrew({ display: "swap", preload: false, subsets: ["hebrew"] })
const armenian = Noto_Sans_Armenian({ display: "swap", preload: false, subsets: ["armenian"] })
const georgian = Noto_Sans_Georgian({ display: "swap", preload: false, subsets: ["georgian"] })
const devanagari = Noto_Sans_Devanagari({ display: "swap", preload: false, subsets: ["devanagari"] })
const bengali = Noto_Sans_Bengali({ display: "swap", preload: false, subsets: ["bengali"] })
const thai = Noto_Sans_Thai({ display: "swap", preload: false, subsets: ["thai"] })
const lao = Noto_Sans_Lao({ display: "swap", preload: false, subsets: ["lao"] })
const khmer = Noto_Sans_Khmer({ display: "swap", preload: false, subsets: ["khmer"] })
const myanmar = Noto_Sans_Myanmar({ display: "swap", preload: false, subsets: ["myanmar"] })
const ethiopic = Noto_Sans_Ethiopic({ display: "swap", preload: false, subsets: ["ethiopic"] })
const sinhala = Noto_Sans_Sinhala({ display: "swap", preload: false, subsets: ["sinhala"] })
const tamil = Noto_Sans_Tamil({ display: "swap", preload: false, subsets: ["tamil"] })

/** The families, for the end of a font-family list. */
export const mapFontFamily = [arabic, hebrew, armenian, georgian, devanagari, bengali, thai, lao, khmer, myanmar, ethiopic, sinhala, tamil]
  .map((font) => font.style.fontFamily)
  .join(", ")

/** The scripts those fonts cover: names in them can always be drawn. */
export const mapFontScripts =
  /[\p{Script=Arabic}\p{Script=Hebrew}\p{Script=Armenian}\p{Script=Georgian}\p{Script=Devanagari}\p{Script=Bengali}\p{Script=Thai}\p{Script=Lao}\p{Script=Khmer}\p{Script=Myanmar}\p{Script=Ethiopic}\p{Script=Sinhala}\p{Script=Tamil}]/u
