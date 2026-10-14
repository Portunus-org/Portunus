"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

/** Setting up SSO is done inside the settings window now; an old link still finds it. */
export default function SSOSetupPage() {
  const router = useRouter()
  React.useEffect(() => router.replace("/#settings/sso"), [router])
  return null
}
