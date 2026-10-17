/**
 * Whether this computer can draw a name. Names on the map are written as
 * they are where the place is, in dozens of scripts. The panel brings fonts
 * for the common ones (map-fonts); for the rest, a system without a font
 * shows rows of boxes instead, and the map then uses the Latin name. There is no way to ask
 * a browser which scripts it has fonts for, so it is measured: in a real
 * font a wide letter and a narrow sign of the same script differ in width,
 * while the boxes drawn for missing letters are all alike.
 */

// [letters of the script, a wide one, a narrow one], for scripts the panel
// brings no font of its own for (see map-fonts). Scripts whose letters all
// have one width (Chinese, Japanese, Korean) cannot be told this way and are
// taken to be present; so are Latin, Cyrillic and Greek.
const scripts: [RegExp, string, string][] = [
  [/\p{Script=Tibetan}/u, "ཀ", "་"],
  [/\p{Script=Mongolian}/u, "ᠮ", "᠂"],
  [/\p{Script=Thaana}/u, "ހ", "ް"],
]

const known = new Map<RegExp, boolean>()
let ctx: CanvasRenderingContext2D | null | undefined

/** Whether every script used in text can be drawn here. */
export function canDraw(text: string): boolean {
  for (const [script, wide, narrow] of scripts) {
    if (!script.test(text)) continue
    let ok = known.get(script)
    if (ok === undefined) {
      if (ctx === undefined) ctx = typeof document === "undefined" ? null : document.createElement("canvas").getContext("2d")
      if (!ctx) return true
      ctx.font = `32px ${getComputedStyle(document.body).fontFamily}`
      ok = Math.abs(ctx.measureText(wide).width - ctx.measureText(narrow).width) > 0.5
      known.set(script, ok)
    }
    if (!ok) return false
  }
  return true
}
