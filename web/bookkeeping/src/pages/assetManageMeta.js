import { LOCAL_ICON_PATHS } from "../assets/localIcons";

export const SUB_TYPE_META = [
  {
    assetType: "asset",
    subType: "现金",
    title: "现金",
    description: "现金钱包",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_cash },
    presets: []
  },
  {
    assetType: "asset",
    subType: "储蓄卡",
    title: "储蓄卡",
    description: "银行卡",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_savings },
    presets: [
      "icbc", "abc", "ccb", "boc", "bcom", "psbc", "cmb", "spdb", "cgb", "bosh", "pingan", "cmbc"
    ]
  },
  {
    assetType: "liability",
    subType: "信用卡",
    title: "信用卡",
    description: "信用卡/花呗/白条",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_credit },
    presets: [
      "icbc", "abc", "ccb", "boc", "bcom", "psbc", "cmb", "spdb", "cgb", "bosh", "pingan", "cmbc"
    ]
  },
  {
    assetType: "asset",
    subType: "虚拟账户",
    title: "虚拟账户",
    description: "支付宝/微信",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_virtual },
    presets: ["alipay", "wechat"]
  },
  {
    assetType: "asset",
    subType: "债权",
    title: "债权",
    description: "应收/借出",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_claim },
    presets: []
  },
  {
    assetType: "liability",
    subType: "欠款",
    title: "欠款",
    description: "借入欠款",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_debt },
    presets: []
  },
  {
    assetType: "liability",
    subType: "消费贷款",
    title: "消费贷款",
    description: "消费分期",
    icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.sub_consumer_loan },
    presets: []
  }
];

export const PRESET_META_MAP = {
  icbc: { code: "icbc", name: "工商银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_icbc } },
  abc: { code: "abc", name: "农业银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_abc } },
  ccb: { code: "ccb", name: "建设银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_ccb } },
  boc: { code: "boc", name: "中国银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_boc } },
  bcom: { code: "bcom", name: "交通银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_bcom } },
  psbc: { code: "psbc", name: "邮储银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_psbc } },
  cmb: { code: "cmb", name: "招商银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_cmb } },
  spdb: { code: "spdb", name: "浦发银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_spdb } },
  cgb: { code: "cgb", name: "广发银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_cgb } },
  bosh: { code: "bosh", name: "上海银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_bosh } },
  pingan: { code: "pingan", name: "平安银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_pingan } },
  cmbc: { code: "cmbc", name: "民生银行", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.bank_cmbc } },
  alipay: { code: "alipay", name: "支付宝", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.preset_alipay } },
  wechat: { code: "wechat", name: "微信", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.preset_wechat } },
  other: { code: "other", name: "其他", icon: { bg: "#f3f4f6", src: LOCAL_ICON_PATHS.preset_other } }
};

export const SUB_TYPE_ORDER = ["现金", "储蓄卡", "虚拟账户", "债权", "信用卡", "欠款", "消费贷款"];

export function findSubTypeMeta(subType) {
  return SUB_TYPE_META.find((it) => it.subType === subType) || null;
}

export function getPresetListBySubType(subType) {
  const meta = findSubTypeMeta(subType);
  if (!meta || !Array.isArray(meta.presets)) {
    return [];
  }
  return meta.presets.map((code) => PRESET_META_MAP[code]).filter(Boolean);
}
