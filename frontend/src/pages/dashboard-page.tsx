import {
  Activity,
  ArrowUpRight,
  BellRing,
  BookOpenText,
  Database,
  FileSearch,
  Server,
  Workflow,
} from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const summaryCards = [
  {
    label: "活跃告警",
    value: "--",
    note: "等待告警源接入",
    icon: BellRing,
    tone: "text-rose-600 bg-rose-50",
  },
  {
    label: "开放故障",
    value: "--",
    note: "Incident Center 未启用",
    icon: Activity,
    tone: "text-amber-600 bg-amber-50",
  },
  {
    label: "知识文档",
    value: "--",
    note: "等待知识库初始化",
    icon: BookOpenText,
    tone: "text-sky-600 bg-sky-50",
  },
  {
    label: "数据源健康",
    value: "--",
    note: "尚未配置数据源",
    icon: Database,
    tone: "text-emerald-600 bg-emerald-50",
  },
];

const readiness = [
  { label: "HTTP 服务", detail: "基础健康检查可用", ready: true },
  { label: "用户与认证", detail: "计划在 Phase 1 接入", ready: false },
  { label: "知识与检索", detail: "计划在 Phase 1 接入", ready: false },
  { label: "分析数据源", detail: "计划在 Phase 2 接入", ready: false },
];

export function DashboardPage() {
  return (
    <div className="mx-auto max-w-[1500px] space-y-7">
      <section className="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
        <div>
          <p className="text-sm font-medium text-cyan-700">运行工作台</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight text-slate-950">
            平台总览
          </h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-500">
            汇总告警、故障、知识和数据源状态。当前处于工程初始化阶段，未接入生产数据。
          </p>
        </div>
        <div className="inline-flex w-fit items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs text-slate-500 shadow-sm">
          <span className="size-2 rounded-full bg-emerald-500 shadow-[0_0_0_4px_rgba(16,185,129,.12)]" />
          平台基础服务可用
        </div>
      </section>

      <section
        className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
        aria-label="关键指标"
      >
        {summaryCards.map((item) => (
          <Card
            key={item.label}
            className="border-slate-200/80 shadow-none transition-shadow hover:shadow-panel"
          >
            <CardContent className="p-5">
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-500">
                    {item.label}
                  </p>
                  <p className="mt-3 text-3xl font-semibold tracking-tight text-slate-950">
                    {item.value}
                  </p>
                </div>
                <div
                  className={`grid size-10 place-items-center rounded-xl ${item.tone}`}
                >
                  <item.icon className="size-5" aria-hidden="true" />
                </div>
              </div>
              <p className="mt-4 text-xs text-slate-400">{item.note}</p>
            </CardContent>
          </Card>
        ))}
      </section>

      <section className="grid gap-6 xl:grid-cols-[1.45fr_0.8fr]">
        <Card className="border-slate-200/80 shadow-none">
          <CardHeader className="flex-row items-start justify-between space-y-0">
            <div>
              <CardTitle>最近分析</CardTitle>
              <CardDescription className="mt-1.5">
                用户发起的知识、日志与 Kubernetes 分析任务
              </CardDescription>
            </div>
            <span className="rounded-full bg-slate-100 px-2.5 py-1 text-[11px] font-medium text-slate-500">
              0 tasks
            </span>
          </CardHeader>
          <CardContent>
            <div className="grid min-h-72 place-items-center rounded-xl border border-dashed border-slate-200 bg-slate-50/70 p-8 text-center">
              <div>
                <div className="mx-auto grid size-12 place-items-center rounded-2xl bg-white text-slate-400 shadow-sm ring-1 ring-slate-200">
                  <FileSearch className="size-5" aria-hidden="true" />
                </div>
                <p className="mt-4 text-sm font-semibold text-slate-700">
                  暂无分析记录
                </p>
                <p className="mx-auto mt-2 max-w-sm text-xs leading-5 text-slate-400">
                  数据源与分析能力将在后续 Task
                  中按设计文档顺序接入，这里不会展示模拟生产数据。
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-200/80 shadow-none">
          <CardHeader>
            <CardTitle>能力就绪度</CardTitle>
            <CardDescription>当前研发阶段的真实可用状态</CardDescription>
          </CardHeader>
          <CardContent className="space-y-1">
            {readiness.map((item) => (
              <div
                key={item.label}
                className="flex items-center gap-3 rounded-lg px-2 py-3 hover:bg-slate-50"
              >
                <span
                  className={`size-2 rounded-full ${item.ready ? "bg-emerald-500" : "bg-slate-300"}`}
                />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium text-slate-700">
                    {item.label}
                  </p>
                  <p className="mt-0.5 truncate text-xs text-slate-400">
                    {item.detail}
                  </p>
                </div>
                <ArrowUpRight
                  className="size-4 text-slate-300"
                  aria-hidden="true"
                />
              </div>
            ))}
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-4 md:grid-cols-3">
        {[
          {
            title: "数据源接入",
            detail: "统一管理日志、Kubernetes 与指标连接",
            icon: Server,
          },
          {
            title: "工作流编排",
            detail: "将只读分析步骤组织为可观察 DAG",
            icon: Workflow,
          },
          {
            title: "证据化报告",
            detail: "区分事实、规则、知识依据与模型推测",
            icon: FileSearch,
          },
        ].map((item) => (
          <div
            key={item.title}
            className="flex items-center gap-4 rounded-xl border border-slate-200/80 bg-white p-4"
          >
            <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-slate-950 text-cyan-300">
              <item.icon className="size-5" aria-hidden="true" />
            </div>
            <div>
              <p className="text-sm font-semibold text-slate-800">
                {item.title}
              </p>
              <p className="mt-1 text-xs leading-5 text-slate-400">
                {item.detail}
              </p>
            </div>
          </div>
        ))}
      </section>
    </div>
  );
}
