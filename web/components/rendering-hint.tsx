"use client"

import * as React from "react"

const KEY = "portunus:rendering"

/** Whether the browser draws without a graphics card (software rendering). */
function softwareRendering(): boolean {
  try {
    const gl = document.createElement("canvas").getContext("webgl")
    if (!gl) return true
    // Firefox names the renderer in the plain parameter and warns about
    // the extension; Chromium says only "WebKit WebGL" there and needs it.
    let renderer = String(gl.getParameter(gl.RENDERER) ?? "")
    if (!renderer || /^webkit webgl$/i.test(renderer)) {
      const info = gl.getExtension("WEBGL_debug_renderer_info")
      if (info) renderer = String(gl.getParameter(info.UNMASKED_RENDERER_WEBGL))
    }
    gl.getExtension("WEBGL_lose_context")?.loseContext()
    return /swiftshader|llvmpipe|software|basic render/i.test(renderer)
  } catch {
    return false
  }
}

/**
 * Marks the page when the browser has no hardware acceleration. Blurring
 * what is behind a panel costs a graphics card next to nothing, but drawn
 * on the processor it has to be redone for the whole screen on every frame;
 * the styles then dim the background without blurring it (globals.css).
 */
export function RenderingHint() {
  React.useEffect(() => {
    let mode: string | null = null
    try {
      mode = sessionStorage.getItem(KEY)
    } catch {}
    if (mode === null) {
      mode = softwareRendering() ? "software" : "gpu"
      try {
        sessionStorage.setItem(KEY, mode)
      } catch {}
    }
    document.documentElement.dataset.rendering = mode
  }, [])
  return null
}
