"use client"

import * as React from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { ArrowLeftIcon, ArrowRightIcon, InfoIcon } from "lucide-react"

import { CopyButton } from "@/components/copy-button"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { docs, findDoc, type DocBlock } from "@/lib/docs"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"

const INLINE = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g

/** Renders the inline markup of doc strings: **bold**, `code`, [link](/path/). */
function Inline({ text }: { text: string }) {
  return (
    <>
      {text.split(INLINE).map((part, i) => {
        if (part.startsWith("**")) {
          return (
            <strong key={i} className="font-medium text-foreground">
              {part.slice(2, -2)}
            </strong>
          )
        }
        if (part.startsWith("`")) {
          return (
            <code key={i} className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em] text-foreground">
              {part.slice(1, -1)}
            </code>
          )
        }
        const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part)
        if (link) {
          return (
            <Link key={i} href={link[2]} className="font-medium text-foreground underline underline-offset-4">
              {link[1]}
            </Link>
          )
        }
        return part
      })}
    </>
  )
}

function Block({ block }: { block: DocBlock }) {
  if ("h" in block) return <h3 className="mt-4 text-lg font-semibold text-foreground">{block.h}</h3>
  if ("p" in block) {
    return (
      <p>
        <Inline text={block.p} />
      </p>
    )
  }
  if ("ul" in block || "ol" in block) {
    const items = "ul" in block ? block.ul : block.ol
    const List = "ul" in block ? "ul" : "ol"
    return (
      <List className={cn("grid gap-2 pl-5", "ul" in block ? "list-disc" : "list-decimal")}>
        {items.map((item, i) => (
          <li key={i} className="pl-1">
            <Inline text={item} />
          </li>
        ))}
      </List>
    )
  }
  if ("code" in block) {
    return (
      <div className="relative">
        <pre className="overflow-x-auto rounded-lg bg-muted p-4 pr-12 font-mono text-xs leading-relaxed text-foreground">{block.code}</pre>
        <CopyButton value={block.code} className="absolute top-2 right-2 text-muted-foreground" />
      </div>
    )
  }
  return (
    <Alert>
      <InfoIcon />
      <AlertDescription>
        <p>
          <Inline text={block.note} />
        </p>
      </AlertDescription>
    </Alert>
  )
}

/** One documentation topic with the list of topics beside it. */
export function DocView({ slug }: { slug: string }) {
  const { t, locale } = useI18n()
  const router = useRouter()
  const topic = findDoc(slug)
  const index = docs.indexOf(topic)
  const prev = docs[index - 1]
  const next = docs[index + 1]

  return (
    <div className="grid gap-6 px-4 lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-10 lg:px-6">
      <nav aria-label={t("docs.title")} className="hidden lg:block">
        <ul className="sticky top-6 grid gap-1">
          {docs.map((d) => (
            <li key={d.slug}>
              <Button
                asChild
                variant="ghost"
                size="sm"
                className={cn("w-full justify-start font-normal text-muted-foreground", d.slug === topic.slug && "bg-muted font-medium text-foreground")}
              >
                <Link href={`/docs/${d.slug}/`} aria-current={d.slug === topic.slug ? "page" : undefined}>
                  {d.title[locale]}
                </Link>
              </Button>
            </li>
          ))}
        </ul>
      </nav>

      <div className="lg:hidden">
        <Select value={topic.slug} onValueChange={(v) => router.push(`/docs/${v}/`)}>
          <SelectTrigger className="w-full" aria-label={t("docs.title")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {docs.map((d) => (
                <SelectItem key={d.slug} value={d.slug}>
                  {d.title[locale]}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      <article className="grid max-w-2xl gap-4 text-sm leading-relaxed text-muted-foreground">
        <header className="grid gap-1">
          {/* The page heading (h1) is in the site header. */}
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">{topic.title[locale]}</h2>
          <p>{topic.summary[locale]}</p>
        </header>
        {topic.body[locale].map((block, i) => (
          <Block key={i} block={block} />
        ))}
        <footer className="mt-6 flex justify-between gap-2 border-t pt-4">
          {prev ? (
            <Button asChild variant="outline" size="sm">
              <Link href={`/docs/${prev.slug}/`}>
                <ArrowLeftIcon />
                {prev.title[locale]}
              </Link>
            </Button>
          ) : (
            <span />
          )}
          {next && (
            <Button asChild variant="outline" size="sm">
              <Link href={`/docs/${next.slug}/`}>
                {next.title[locale]}
                <ArrowRightIcon />
              </Link>
            </Button>
          )}
        </footer>
      </article>
    </div>
  )
}
