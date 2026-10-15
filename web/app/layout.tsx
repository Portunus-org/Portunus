import type { Metadata, Viewport } from "next"
import { Geist, Geist_Mono, Source_Serif_4 } from "next/font/google"
import { ThemeProvider } from "next-themes"

import { FreshBuild } from "@/components/fresh-build"
import { KeyboardAware } from "@/components/keyboard-aware"
import { RenderingHint } from "@/components/rendering-hint"

import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { I18nProvider } from "@/lib/i18n"
import "./globals.css"

// Fonts are downloaded at build time and served by the panel itself:
// no requests to Google from the admin's browser.
const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin", "cyrillic"],
})

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin", "cyrillic"],
})

// Headings are set in a serif, the interface in a sans: titles read as
// titles without being larger or heavier. Source Serif has both Latin and
// Cyrillic, so the two interface languages look alike.
const sourceSerif = Source_Serif_4({
  variable: "--font-source-serif",
  subsets: ["latin", "cyrillic"],
})

export const metadata: Metadata = {
  title: { default: "Portunus", template: "%s | Portunus" },
  description: "Self-hosted sing-box control panel",
  robots: { index: false, follow: false },
  referrer: "no-referrer",
}

// On a phone the page is made shorter while the keyboard is open, rather
// than the keyboard being laid over it (Chrome on Android; Safari covers
// the page either way, and KeyboardAware brings the field into view).
export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  interactiveWidget: "resizes-content",
}

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${geistSans.variable} ${geistMono.variable} ${sourceSerif.variable} h-full antialiased`}
    >
      <body className="min-h-full">
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
          <RenderingHint />
          <FreshBuild />
          <KeyboardAware />
          <I18nProvider>
            <TooltipProvider>
              {children}
              {/* On the right unless a panel is open there: each note picks its side as it appears (lib/toast). */}
              <Toaster position="bottom-right" />
            </TooltipProvider>
          </I18nProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
