"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

/** The settings are a window over the panel now; an old link to this page opens it. */
export default function SettingsPage() {
  const router = useRouter()
  React.useEffect(() => {
    const section = window.location.hash.slice(1)
    router.replace("/#settings" + (section ? "/" + section : ""))
  }, [router])
  return null
}
