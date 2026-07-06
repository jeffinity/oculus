import { useMemo } from "react";

function sanitizeSvg(svg) {
  if (!svg) return "";
  return svg.replace(/<svg([^>]*)>/i, (full, attrs) => {
    const nextAttrs = attrs
      .replaceAll(/\sclass="[^"]*"/g, "")
      .replaceAll(/\sstyle="[^"]*"/g, "")
      .replaceAll(/\swidth="[^"]*"/g, "")
      .replaceAll(/\sheight="[^"]*"/g, "");
    return `<svg${nextAttrs}>`;
  });
}

export default function IconfontSvg({ svg, className = "" }) {
  const html = useMemo(() => sanitizeSvg(svg), [svg]);
  if (!html) return null;
  return <span className={`iconfont-svg ${className}`.trim()} dangerouslySetInnerHTML={{ __html: html }} />;
}
