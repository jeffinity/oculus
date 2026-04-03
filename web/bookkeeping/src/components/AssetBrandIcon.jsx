import IconfontSvg from "./IconfontSvg";

export default function AssetBrandIcon({ icon, className = "" }) {
  const bg = "transparent";
  const text = icon?.text || "";
  return (
    <span className={`asset-brand-icon ${className}`.trim()} style={{ background: bg }}>
      {icon?.src ? <img src={icon.src} alt="" className="asset-brand-icon-img" /> : null}
      {!icon?.src && icon?.svg ? <IconfontSvg svg={icon.svg} className="asset-brand-icon-svg" /> : null}
      {!icon?.src && !icon?.svg ? text : null}
    </span>
  );
}
