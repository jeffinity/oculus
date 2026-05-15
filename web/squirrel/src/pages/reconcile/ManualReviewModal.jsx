/* eslint-disable max-lines-per-function */
import { Button, Descriptions, Divider, Input, InputNumber, Modal, Space, Spin, Tag, Typography } from "antd";

export default function ManualReviewModal({
  activeManualRow,
  buyerPaidColIndex,
  copyOrderNo,
  formatAmount,
  manualModalOpen,
  manualRemarkOptions,
  manualRemarkOptionsLoading,
  manualPreview,
  manualRemark,
  manualSaving,
  onCancel,
  onDeleteRemarkOption,
  onRemarkChange,
  onSelectRemarkOption,
  onRefundAmountChange,
  onRefundQtyChange,
  onSubmit,
  refundAmount,
  refundQty,
  salesOrderNoColIndex,
  salesPayAmountColIndex,
  shipQtyColIndex,
  valueFromRow,
  OrderNoCopyText
}) {
  let remarkOptionsContent = <Typography.Text type="secondary">暂无常用备注</Typography.Text>;
  if (manualRemarkOptionsLoading) {
    remarkOptionsContent = <Spin size="small" />;
  } else if (manualRemarkOptions.length > 0) {
    remarkOptionsContent = (
      <Space wrap size={[8, 8]}>
        {manualRemarkOptions.map((item) => (
          <Tag
            key={item.content}
            closable
            color={manualRemark === item.content ? "processing" : "default"}
            onClick={() => onSelectRemarkOption(item.content)}
            onClose={(e) => {
              e.preventDefault();
              onDeleteRemarkOption(item.content);
            }}
            style={{ cursor: "pointer", userSelect: "none" }}
          >
            {item.content}
          </Tag>
        ))}
      </Space>
    );
  }

  return (
    <Modal
      title="人工核查"
      open={manualModalOpen}
      onCancel={onCancel}
      footer={[
        <Button key="cancel" onClick={onCancel}>
          取消
        </Button>,
        <Button key="ignore" danger loading={manualSaving} onClick={() => onSubmit(true)}>
          忽略本条数据
        </Button>,
        <Button key="submit" type="primary" loading={manualSaving} onClick={() => onSubmit(false)}>
          提交
        </Button>
      ]}
    >
      <Descriptions column={1} size="small" bordered>
        <Descriptions.Item label="子单原始单号">
          <OrderNoCopyText value={valueFromRow(activeManualRow, salesOrderNoColIndex)} onCopy={copyOrderNo} />
        </Descriptions.Item>
        <Descriptions.Item label="订单支付金额">
          {valueFromRow(activeManualRow, salesPayAmountColIndex) || "-"}
        </Descriptions.Item>
        <Descriptions.Item label="买家实付">
          {valueFromRow(activeManualRow, buyerPaidColIndex) || "-"}
        </Descriptions.Item>
        <Descriptions.Item label="实发数量">
          {valueFromRow(activeManualRow, shipQtyColIndex) || "-"}
        </Descriptions.Item>
      </Descriptions>
      <Divider />
      <Space direction="vertical" style={{ width: "100%" }}>
        <div>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
            <Typography.Text>退款数量</Typography.Text>
            <Button
              size="small"
              type="link"
              style={{ paddingInline: 0 }}
              onClick={() => {
                onRefundQtyChange(valueFromRow(activeManualRow, shipQtyColIndex) || 0);
                onRefundAmountChange(valueFromRow(activeManualRow, buyerPaidColIndex) || 0);
              }}
            >
              全部退款
            </Button>
          </div>
          <InputNumber
            style={{ width: "100%", marginTop: 6 }}
            value={refundQty}
            onChange={onRefundQtyChange}
            precision={0}
            step={1}
            controls={false}
          />
        </div>
        <div>
          <Typography.Text>退款金额</Typography.Text>
          <InputNumber
            style={{ width: "100%", marginTop: 6 }}
            value={refundAmount}
            onChange={onRefundAmountChange}
            step={0.01}
            controls={false}
          />
        </div>
        <div>
          <Typography.Text>备注</Typography.Text>
          <Input.TextArea
            style={{ marginTop: 6 }}
            value={manualRemark}
            onChange={onRemarkChange}
            placeholder="请输入人工核查备注"
            autoSize={{ minRows: 3, maxRows: 5 }}
            maxLength={500}
          />
          <div style={{ marginTop: 10 }}>
            <Typography.Text type="secondary">常用备注</Typography.Text>
            <div style={{ marginTop: 8, minHeight: 32 }}>{remarkOptionsContent}</div>
          </div>
        </div>
      </Space>
      <Divider />
      <Descriptions column={1} size="small" bordered>
        <Descriptions.Item label="结算数量">{manualPreview ? manualPreview.settleQty : "-"}</Descriptions.Item>
        <Descriptions.Item label="结算金额">
          {manualPreview ? formatAmount(manualPreview.settleAmount) : "-"}
        </Descriptions.Item>
      </Descriptions>
    </Modal>
  );
}
