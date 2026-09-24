import type { Metadata } from "next"
import { Fraunces, Geist, Geist_Mono, Source_Serif_4 } from "next/font/google"
import { ThemeProvider } from "next-themes"

import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
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

const fraunces = Fraunces({
  variable: "--font-fraunces",
  subsets: ["latin"],
  axes: ["opsz"],
})

// Cyrillic fallback for the serif face; only fetched when Cyrillic glyphs appear.
const sourceSerif = Source_Serif_4({
  variable: "--font-source-serif",
  subsets: ["cyrillic"],
  axes: ["opsz"],
  preload: false,
})

export const metadata: Metadata = {
  title: { default: "Portunus", template: "%s · Portunus" },
  description: "Self-hosted sing-box control panel",
  robots: { index: false, follow: false },
  referrer: "no-referrer",
}

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${geistSans.variable} ${geistMono.variable} ${fraunces.variable} ${sourceSerif.variable} h-full antialiased`}
    >
      <body className="min-h-full">
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
          <TooltipProvider delayDuration={300}>
            {children}
            <Toaster position="bottom-right" />
          </TooltipProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
