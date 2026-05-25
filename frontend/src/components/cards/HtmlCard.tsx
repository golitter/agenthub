interface HtmlCardProps {
  content: string
}

export function HtmlCard({ content }: HtmlCardProps) {
  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <iframe sandbox="" srcDoc={content} className="h-64 w-full border-0" title="HTML Preview" />
    </div>
  )
}
