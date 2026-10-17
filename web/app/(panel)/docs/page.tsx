"use client"

import Link from "next/link"

import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { docs } from "@/lib/docs"
import { useI18n } from "@/lib/i18n"

export default function DocsIndexPage() {
  const { locale } = useI18n()
  return (
    <div className="grid gap-4 px-4 sm:grid-cols-2 lg:px-6 xl:grid-cols-3">
      {docs.map((d) => (
        <Link key={d.slug} href={`/docs/${d.slug}/`} className="rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <Card className="h-full transition-colors hover:bg-muted/50">
            <CardHeader>
              <CardTitle>{d.title[locale]}</CardTitle>
              <CardDescription>{d.summary[locale]}</CardDescription>
            </CardHeader>
          </Card>
        </Link>
      ))}
    </div>
  )
}
