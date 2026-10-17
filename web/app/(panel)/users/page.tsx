"use client"

import * as React from "react"
import { useRouter } from "next/navigation"

/** Accounts are a section of the settings window now; an old link still finds them. */
export default function UsersPage() {
  const router = useRouter()
  React.useEffect(() => router.replace("/#settings/accounts"), [router])
  return null
}
