import { RightOutlined } from "@ant-design/icons";

import AssetBrandIcon from "../components/AssetBrandIcon";
import MobileHeader from "../components/MobileHeader";

import { SUB_TYPE_META } from "./assetManageMeta";
import { backToAssetManage, getCurrentYM, navigateToView } from "./assetManageNav";

function toNext(meta, ym) {
  if (Array.isArray(meta.presets) && meta.presets.length > 0) {
    navigateToView("asset-preset-select", {
      ym,
      sub_type: meta.subType,
      asset_type: meta.assetType
    });
    return;
  }
  navigateToView("asset-detail-form", {
    mode: "create",
    ym,
    sub_type: meta.subType,
    asset_type: meta.assetType,
    preset_code: ""
  });
}

export default function AssetCategorySelectPage() {
  const ym = getCurrentYM();

  return (
    <div className="mobile-page-wrap">
      <MobileHeader title="选择资产分类" onBack={() => backToAssetManage(ym)} />
      <div className="mobile-page-content">
        <div className="asset-option-list asset-category-option-list">
          {SUB_TYPE_META.map((meta) => (
            <button key={meta.subType} type="button" className="asset-option-row" onClick={() => toNext(meta, ym)}>
              <AssetBrandIcon icon={meta.icon} />
              <span className="asset-option-main">
                <span className="asset-option-title">{meta.title}</span>
                <span className="asset-option-desc">{meta.description}</span>
              </span>
              <RightOutlined className="asset-option-arrow" />
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
