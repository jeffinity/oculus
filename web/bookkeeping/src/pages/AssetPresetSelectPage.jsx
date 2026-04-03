import { RightOutlined } from "@ant-design/icons";

import AssetBrandIcon from "../components/AssetBrandIcon";
import MobileHeader from "../components/MobileHeader";

import { PRESET_META_MAP, findSubTypeMeta, getPresetListBySubType } from "./assetManageMeta";
import { backToCategorySelect, getCurrentYM, getSearch, navigateToView } from "./assetManageNav";

function navigateToForm(item, ym, subType, assetType) {
  const presetCode = item.code === "other" ? "" : item.code;
  navigateToView("asset-detail-form", {
    mode: "create",
    ym,
    sub_type: subType,
    asset_type: assetType,
    preset_code: presetCode
  });
}

export default function AssetPresetSelectPage() {
  const search = getSearch();
  const ym = getCurrentYM();
  const subType = `${search.get("sub_type") || ""}`.trim();
  const assetType = `${search.get("asset_type") || ""}`.trim();
  const subMeta = findSubTypeMeta(subType);
  const presets = getPresetListBySubType(subType);

  return (
    <div className="mobile-page-wrap">
      <MobileHeader title={subMeta ? subMeta.title : "选择预置"} onBack={() => backToCategorySelect(ym)} />
      <div className="mobile-page-content">
        <div className="asset-option-list asset-preset-option-list">
          {presets.map((item) => (
            <button
              key={item.code}
              type="button"
              className="asset-option-row"
              onClick={() => navigateToForm(item, ym, subType, assetType)}
            >
              <AssetBrandIcon icon={item.icon} />
              <span className="asset-option-main">
                <span className="asset-option-title">{item.name}</span>
              </span>
              <RightOutlined className="asset-option-arrow" />
            </button>
          ))}
          <button
            type="button"
            className="asset-option-row asset-option-other"
            onClick={() => navigateToForm(PRESET_META_MAP.other, ym, subType, assetType)}
          >
            <AssetBrandIcon icon={PRESET_META_MAP.other.icon} />
            <span className="asset-option-main">
              <span className="asset-option-title">其他</span>
            </span>
            <RightOutlined className="asset-option-arrow" />
          </button>
        </div>
      </div>
    </div>
  );
}
