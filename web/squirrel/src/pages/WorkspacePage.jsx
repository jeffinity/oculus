/* eslint-disable max-lines-per-function, complexity, max-lines */
import { DeleteOutlined, InboxOutlined, PlusOutlined } from "@ant-design/icons";
import {
  Button,
  Card,
  Empty,
  Input,
  Layout,
  List,
  Modal,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  Upload,
  message
} from "antd";
import { useCallback, useEffect, useMemo, useState } from "react";

const { Sider, Content } = Layout;
const { Dragger } = Upload;

const PAGE_SIZE = 10;
const SECTION_META = [
  { key: "sales", title: "销售明细", accent: "#136f63" },
  { key: "wechat", title: "微信支付明细", accent: "#00a870" },
  { key: "alipay", title: "支付宝支付明细", accent: "#1476ff" }
];

function getDefaultMonthTag() {
  const now = new Date();
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  return `${y}-${m}`;
}

function getDefaultTaskTitle() {
  const monthTag = getDefaultMonthTag();
  return `${monthTag} 月度任务`;
}

function getTaskField(task, camel, snake) {
  return task?.[camel] ?? task?.[snake] ?? "";
}

function formatDateToMinute(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  const y = date.getFullYear();
  const m = `${date.getMonth() + 1}`.padStart(2, "0");
  const d = `${date.getDate()}`.padStart(2, "0");
  const hh = `${date.getHours()}`.padStart(2, "0");
  const mm = `${date.getMinutes()}`.padStart(2, "0");
  return `${y}-${m}-${d} ${hh}:${mm}`;
}

function toBase64(arrayBuffer) {
  const bytes = new Uint8Array(arrayBuffer);
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode.apply(null, chunk);
  }
  return window.btoa(binary);
}

async function requestJSON(url, options = {}) {
  const res = await fetch(url, options);
  if (!res.ok) {
    let detail = "";
    try {
      const json = await res.json();
      detail = json?.message || json?.error || "";
    } catch {
      detail = "";
    }
    throw new Error(detail || `request failed (${res.status})`);
  }
  if (res.status === 204) return {};
  return res.json();
}

async function listTasks() {
  const json = await requestJSON("/api/v1/squirrel/tasks");
  return json.items || [];
}

async function createTask(payload) {
  const json = await requestJSON("/api/v1/squirrel/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  return json.item;
}

async function deleteTask(taskId) {
  await requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}`, {
    method: "DELETE"
  });
}

async function fetchSectionRows(taskId, section, page = 1, pageSize = PAGE_SIZE) {
  const q = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize)
  });
  return requestJSON(
    `/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/sections/${encodeURIComponent(section)}/rows?${q.toString()}`
  );
}

async function uploadSectionFile(taskId, section, file) {
  const arrayBuffer = await file.arrayBuffer();
  const payload = {
    taskId,
    section,
    filename: file.name,
    fileContent: toBase64(arrayBuffer)
  };
  return requestJSON(
    `/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/sections/${encodeURIComponent(section)}/upload`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    }
  );
}

async function clearSection(taskId, section) {
  await requestJSON(`/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/sections/${encodeURIComponent(section)}/data`, {
    method: "DELETE"
  });
}

async function clearSectionFile(taskId, section, filename) {
  const q = new URLSearchParams();
  if (filename) q.set("filename", filename);
  await requestJSON(
    `/api/v1/squirrel/tasks/${encodeURIComponent(taskId)}/sections/${encodeURIComponent(section)}/data?${q.toString()}`,
    {
      method: "DELETE"
    }
  );
}

function buildColumns(sectionState) {
  const dataColumns = (sectionState.columns || []).map((name, idx) => ({
    title: name || `列${idx + 1}`,
    dataIndex: `c_${idx}`,
    key: `c_${idx}`,
    ellipsis: true,
    width: 180
  }));
  return [
    {
      title: "文件名",
      dataIndex: "fileName",
      key: "fileName",
      width: 220,
      fixed: "left",
      ellipsis: true
    },
    {
      title: "行号",
      dataIndex: "rowNo",
      key: "rowNo",
      width: 90,
      fixed: "left"
    },
    ...dataColumns
  ];
}

function normalizeRows(rows = [], columnCount = 0) {
  return rows.map((row) => {
    const values = [...(row.values || [])];
    while (values.length < columnCount) values.push("");
    const mapped = {
      key: `${row.fileName || row.filename || "unknown"}-${row.rowNo}`,
      fileName: row.fileName || row.filename || "-",
      rowNo: row.rowNo
    };
    for (const [idx, v] of values.entries()) {
      mapped[`c_${idx}`] = v;
    }
    return mapped;
  });
}

function initSectionState() {
  return {
    loading: false,
    page: 1,
    pageSize: PAGE_SIZE,
    total: 0,
    columns: [],
    summary: null,
    summaries: [],
    rows: []
  };
}

async function runUploadRequest({ section, file, onUpload, onSuccess, onError }) {
  try {
    await onUpload(section, file);
    onSuccess?.("ok");
  } catch (error) {
    onError?.(error);
  }
}

function renderSectionBody({ state, tableColumns, tableData, meta, onReload }) {
  if (state.loading) {
    return (
      <div className="section-loading">
        <Spin />
      </div>
    );
  }
  if (!((state.total || 0) > 0)) {
    return null;
  }
  return (
    <div className="section-table-wrap">
      <Table
        columns={tableColumns}
        dataSource={tableData}
        size="small"
        scroll={{ x: "max-content" }}
        pagination={{
          current: state.page,
          pageSize: state.pageSize,
          total: state.total,
          showSizeChanger: false,
          onChange: (page) => onReload(meta.key, page)
        }}
      />
    </div>
  );
}

function SectionCard({ meta, taskId, state, onReload, onClear, onUpload, onDeleteFile }) {
  const hasData = (state.total || 0) > 0;
  const tableColumns = useMemo(() => buildColumns(state), [state]);
  const tableData = useMemo(
    () => normalizeRows(state.rows, state.columns.length),
    [state.rows, state.columns.length]
  );
  const isMultiUploadSection = meta.key === "wechat" || meta.key === "alipay";
  const handleUploadRequest = ({ file, onError, onSuccess }) =>
    runUploadRequest({ section: meta.key, file, onUpload, onSuccess, onError });
  const sectionBody = renderSectionBody({
    state,
    tableColumns,
    tableData,
    meta,
    onReload
  });

  return (
    <Card className="section-card" styles={{ body: { padding: 14 } }}>
      <div className="section-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            {meta.title}
          </Typography.Title>
          {state.summaries?.length > 0 ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              已导入文件: {state.summaries.length} 个
            </Typography.Text>
          ) : (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              暂未导入文件
            </Typography.Text>
          )}
        </div>
        <Space>
          <Button danger disabled={!taskId || !hasData || state.loading} onClick={() => onClear(meta.key)}>
            清空
          </Button>
        </Space>
      </div>
      <div className="section-content">
        {hasData ? (
          <div className="section-upload-bar">
            <div className="section-files">
              {(state.summaries || []).map((item) => (
                <Tag
                  key={`${item.fileId || item.file_id}-${item.filename}`}
                  closable
                  onClose={(e) => {
                    e.preventDefault();
                    onDeleteFile(meta.key, item.filename);
                  }}
                >
                  {item.filename}
                </Tag>
              ))}
            </div>
            <Upload
              disabled={!taskId || state.loading}
              accept=".xlsx"
              multiple={isMultiUploadSection}
              showUploadList={false}
              customRequest={handleUploadRequest}
            >
              <Button size="small" type="default">
                继续上传
              </Button>
            </Upload>
          </div>
        ) : (
          <Dragger
            className="section-dragger section-dragger-empty"
            disabled={!taskId || state.loading}
            accept=".xlsx"
            multiple={isMultiUploadSection}
            showUploadList={false}
            customRequest={handleUploadRequest}
            style={{ borderColor: meta.accent }}
          >
            <p className="ant-upload-drag-icon">
              <InboxOutlined style={{ color: meta.accent }} />
            </p>
            <p className="ant-upload-text">拖入或点击上传 {meta.title} xlsx</p>
            <p className="ant-upload-hint">仅解析 sheet1，保留原始行号与每列内容</p>
          </Dragger>
        )}

        {sectionBody}
      </div>
    </Card>
  );
}

function WorkspacePage() {
  const [msgApi, msgContext] = message.useMessage();

  const [taskLoading, setTaskLoading] = useState(true);
  const [tasks, setTasks] = useState([]);
  const [activeTaskId, setActiveTaskId] = useState("");
  const [draftTitle, setDraftTitle] = useState(getDefaultTaskTitle);
  const [creating, setCreating] = useState(false);

  const [sections, setSections] = useState(() =>
    SECTION_META.reduce((acc, item) => {
      acc[item.key] = initSectionState();
      return acc;
    }, {})
  );

  const activeTask = useMemo(
    () => tasks.find((it) => it.taskId === activeTaskId || it.task_id === activeTaskId) || null,
    [tasks, activeTaskId]
  );

  const activeTaskCreatedAt = formatDateToMinute(getTaskField(activeTask, "createdAt", "created_at"));

  useEffect(() => {
    let alive = true;
    setTaskLoading(true);
    const loadTasks = async () => {
      try {
        const items = await listTasks();
        if (!alive) return;
        setTasks(items || []);
        if (items?.length > 0) {
          const firstId = items[0].taskId || items[0].task_id;
          setActiveTaskId(firstId || "");
        }
      } catch (error) {
        if (!alive) return;
        msgApi.error(error.message || "加载任务失败");
      } finally {
        if (alive) setTaskLoading(false);
      }
    };
    void loadTasks();

    return () => {
      alive = false;
    };
  }, [msgApi]);

  const reloadSection = useCallback(async (section, page = 1) => {
    if (!activeTaskId) return;
    setSections((prev) => ({
      ...prev,
      [section]: {
        ...prev[section],
        loading: true
      }
    }));
    try {
      const json = await fetchSectionRows(activeTaskId, section, page, PAGE_SIZE);
      const columns = json.columns || [];
      const rows = json.rows || [];
      const summaries = json.summaries || [];
      setSections((prev) => ({
        ...prev,
        [section]: {
          ...prev[section],
          loading: false,
          page: json.page || page,
          pageSize: json.pageSize || PAGE_SIZE,
          total: json.total || 0,
          columns,
          rows,
          summary: json.summary || null,
          summaries
        }
      }));
    } catch (err) {
      setSections((prev) => ({
        ...prev,
        [section]: {
          ...prev[section],
          loading: false,
          page,
          rows: [],
          columns: [],
          total: 0,
          summary: null,
          summaries: []
        }
      }));
      msgApi.error(err.message || `${section} 加载失败`);
    }
  }, [activeTaskId, msgApi]);

  useEffect(() => {
    if (!activeTaskId) {
      setSections(
        SECTION_META.reduce((acc, item) => {
          acc[item.key] = initSectionState();
          return acc;
        }, {})
      );
      return;
    }
    for (const item of SECTION_META) {
      reloadSection(item.key, 1);
    }
  }, [activeTaskId, reloadSection]);

  async function reloadTasksAndKeepSelection(preferredTaskId = "") {
    const items = await listTasks();
    setTasks(items || []);
    const picked =
      preferredTaskId ||
      activeTaskId ||
      (items[0] && (items[0].taskId || items[0].task_id)) ||
      "";
    const exists = items.some((it) => (it.taskId || it.task_id) === picked);
    let nextTaskId = "";
    if (exists) {
      nextTaskId = picked;
    } else if (items[0]) {
      nextTaskId = items[0].taskId || items[0].task_id || "";
    }
    setActiveTaskId(nextTaskId);
  }

  async function handleCreateTask() {
    const title = draftTitle.trim();
    if (!title) {
      msgApi.warning("请填写任务标题");
      return;
    }
    setCreating(true);
    try {
      const item = await createTask({
        title,
        monthTag: getDefaultMonthTag()
      });
      const newId = item?.taskId || item?.task_id;
      await reloadTasksAndKeepSelection(newId || "");
      setDraftTitle(getDefaultTaskTitle());
      msgApi.success("任务创建成功");
    } catch (err) {
      msgApi.error(err.message || "任务创建失败");
    } finally {
      setCreating(false);
    }
  }

  function handleDeleteTask(task) {
    const taskId = task.taskId || task.task_id;
    const taskTitle = task.title || "";
    Modal.confirm({
      title: "确认删除该任务吗？",
      content: `删除后将清空其下全部导入文件与解析结果：${taskTitle}`,
      okType: "danger",
      onOk: async () => {
        await deleteTask(taskId);
        await reloadTasksAndKeepSelection("");
        msgApi.success("任务已删除");
      }
    });
  }

  async function handleUpload(section, file) {
    if (!activeTaskId) {
      msgApi.warning("请先创建并选择任务");
      return;
    }
    const ext = file.name?.toLowerCase()?.split(".")?.pop();
    if (ext !== "xlsx") {
      msgApi.warning("仅支持 xlsx 文件");
      return;
    }

    setSections((prev) => ({
      ...prev,
      [section]: { ...prev[section], loading: true }
    }));

    try {
      await uploadSectionFile(activeTaskId, section, file);
      await reloadSection(section, 1);
      await reloadTasksAndKeepSelection(activeTaskId);
      msgApi.success("文件上传并解析完成");
    } catch (err) {
      setSections((prev) => ({
        ...prev,
        [section]: { ...prev[section], loading: false }
      }));
      msgApi.error(err.message || "上传失败");
    }
  }

  function handleClear(section) {
    if (!activeTaskId) return;
    Modal.confirm({
      title: "确认清空该区域数据吗？",
      content: "将删除已导入文件及解析结果，此操作不可撤销。",
      okType: "danger",
      onOk: async () => {
        await clearSection(activeTaskId, section);
        await reloadSection(section, 1);
        await reloadTasksAndKeepSelection(activeTaskId);
        msgApi.success("区域数据已清空");
      }
    });
  }

  function handleDeleteFile(section, filename) {
    if (!activeTaskId || !filename) return;
    Modal.confirm({
      title: "确认删除该文件吗？",
      content: `将按文件名清理该区域的数据：${filename}`,
      okType: "danger",
      onOk: async () => {
        await clearSectionFile(activeTaskId, section, filename);
        await reloadSection(section, 1);
        await reloadTasksAndKeepSelection(activeTaskId);
        msgApi.success("文件数据已清理");
      }
    });
  }

  function openReconcileTab() {
    if (!activeTaskId) {
      msgApi.warning("请先选择任务");
      return;
    }
    const taskSummaries = activeTask?.sectionSummaries || activeTask?.section_summaries || [];
    const importedSectionsFromTask = new Set(
      taskSummaries.map((it) => (it.section || "").toLowerCase()).filter(Boolean)
    );
    const importedSectionsFromPage = new Set(
      SECTION_META.filter((it) => (sections[it.key]?.summaries || []).length > 0).map((it) => it.key)
    );
    const importedSections = new Set([...importedSectionsFromTask, ...importedSectionsFromPage]);
    const missingTitles = SECTION_META.filter((it) => !importedSections.has(it.key)).map((it) => it.title);
    if (missingTitles.length > 0) {
      msgApi.warning(`请先上传${missingTitles.join("、")}后再核算`);
      return;
    }
    const next = new URL(window.location.href);
    next.searchParams.set("view", "reconcile");
    next.searchParams.set("task_id", activeTaskId);
    next.searchParams.set("task_title", activeTask?.title || "任务");
    window.open(next.toString(), "_blank", "noopener,noreferrer");
  }

  return (
    <>
      {msgContext}
      <Layout className="squirrel-layout">
        <Sider width={320} className="task-sider">
          <div className="task-sider-inner">
            <Typography.Title level={3} className="sider-title">
              对账工作区
            </Typography.Title>
            <Card className="task-create-card" styles={{ body: { padding: 12 } }}>
              <Space direction="vertical" style={{ width: "100%" }}>
                <Input
                  value={draftTitle}
                  onChange={(e) => setDraftTitle(e.target.value)}
                  placeholder="2025-03 月度任务"
                  maxLength={128}
                />
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  loading={creating}
                  onClick={handleCreateTask}
                  block
                >
                  创建任务
                </Button>
              </Space>
            </Card>

            <div className="task-list-wrap">
              <Typography.Text className="task-list-title">任务列表</Typography.Text>
              {taskLoading ? (
                <div className="task-loading">
                  <Spin />
                </div>
              ) : (
                <List
                  className="task-list"
                  split={false}
                  dataSource={tasks}
                  locale={{ emptyText: "暂无任务" }}
                  renderItem={(item) => {
                    const taskId = item.taskId || item.task_id;
                    const active = taskId === activeTaskId;
                    const summaries = item.sectionSummaries || item.section_summaries || [];
                    const importedSections = new Set(
                      summaries.map((it) => (it.section || "").toLowerCase()).filter(Boolean)
                    ).size;
                    const hasImported = importedSections > 0;
                    return (
                      <List.Item
                        className={`task-item ${active ? "task-item-active" : ""}`}
                        onClick={() => setActiveTaskId(taskId)}
                        actions={[
                          <Button
                            key="delete"
                            danger
                            type="text"
                            icon={<DeleteOutlined />}
                            onClick={(e) => {
                              e.stopPropagation();
                              handleDeleteTask(item);
                            }}
                          />
                        ]}
                      >
                        <List.Item.Meta
                          title={<span>{item.title}</span>}
                          description={
                            <Space wrap>
                              <Tag color="blue">{item.monthTag || item.month_tag || "-"}</Tag>
                              <Tag color={hasImported ? "green" : "default"}>
                                {hasImported ? `已导入 ${importedSections} 区` : "未导入"}
                              </Tag>
                            </Space>
                          }
                        />
                      </List.Item>
                    );
                  }}
                />
              )}
            </div>
          </div>
        </Sider>

        <Content className="workspace-content">
          {!activeTask ? (
            <div className="workspace-empty">
              <Empty description="请先创建并选择任务" />
            </div>
          ) : (
            <div className="workspace-inner">
              <div className="workspace-title-row">
                <Typography.Title level={3} style={{ margin: 0 }}>
                  {activeTask.title}
                </Typography.Title>
                <Space>
                  <Button type="primary" onClick={openReconcileTab}>
                    核算
                  </Button>
                  <Tag color="geekblue">创建于 {activeTaskCreatedAt}</Tag>
                </Space>
              </div>
              <div className="section-grid">
                {SECTION_META.map((meta) => (
                  <SectionCard
                    key={meta.key}
                    meta={meta}
                    taskId={activeTaskId}
                    state={sections[meta.key]}
                    onReload={reloadSection}
                    onClear={handleClear}
                    onUpload={handleUpload}
                    onDeleteFile={handleDeleteFile}
                  />
                ))}
              </div>
            </div>
          )}
        </Content>
      </Layout>
    </>
  );
}


export default WorkspacePage;
