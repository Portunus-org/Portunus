import { DocView } from "@/components/docs/doc-view"
import { docs } from "@/lib/docs"

// The panel is a static export: every topic is rendered at build time.
export const dynamicParams = false

export function generateStaticParams() {
  return docs.map((d) => ({ slug: d.slug }))
}

export default async function DocPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params
  return <DocView slug={slug} />
}
