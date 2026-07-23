import { useState } from "react";
import {
  Activity,
  BellRing,
  BookOpenText,
  Bot,
  ChevronDown,
  LayoutDashboard,
  Menu,
  Network,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Siren,
  Workflow,
  X,
} from "lucide-react";
import { NavLink, Outlet } from "react-router-dom";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const navigation = [
  { label: "运行总览", icon: LayoutDashboard, to: "/dashboard", enabled: true },
  { label: "智能分析", icon: Bot, to: "/analysis", enabled: true },
  { label: "知识中心", icon: BookOpenText, to: "/knowledge", enabled: true },
  { label: "告警中心", icon: BellRing, enabled: false },
  { label: "工作流", icon: Workflow, to: "/workflows", enabled: true },
  { label: "拓扑视图", icon: Network, to: "/topology", enabled: true },
  { label: "故障中心", icon: Siren, to: "/operations", enabled: true },
  { label: "Linux 主机", icon: Server, to: "/linux-hosts", enabled: true },
  { label: "Linux 分析", icon: Activity, to: "/linux-analysis", enabled: true },
  { label: "配置中心", icon: Settings, to: "/settings", enabled: true },
];

export function AppShell() {
  const [sidebarOpen, setSidebarOpen] = useState(false);

  return (
    <div className="min-h-screen bg-[#eff3f6] text-[#252b3a]">
      <div
        className={cn(
          "fixed inset-0 z-40 bg-[#252b3a]/55 backdrop-blur-sm transition-opacity lg:hidden",
          sidebarOpen ? "opacity-100" : "pointer-events-none opacity-0",
        )}
        aria-hidden="true"
        onClick={() => setSidebarOpen(false)}
      />

      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-50 flex w-72 flex-col border-r border-[#d1dbe5] bg-white text-[#747c8b] shadow-xl transition-transform lg:translate-x-0",
          sidebarOpen ? "translate-x-0" : "-translate-x-full",
        )}
        aria-label="主导航"
      >
        <div className="flex h-20 items-center gap-3 border-b border-[#d1dbe5] px-6">
          <div className="grid size-10 place-items-center rounded-lg bg-[#2d3748] text-brand-400 shadow-sm">
            <Activity className="size-5" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold tracking-wide text-[#252b3a]">
              AI Native AIOps
            </p>
            <p className="text-xs text-[#747c8b]">智能运维分析平台</p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="ml-auto text-[#747c8b] hover:bg-[#eff3f6] hover:text-[#252b3a] lg:hidden"
            onClick={() => setSidebarOpen(false)}
            aria-label="关闭导航"
          >
            <X className="size-5" />
          </Button>
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto px-4 py-6">
          <p className="mb-3 px-3 text-[11px] font-semibold uppercase tracking-[0.18em] text-[#adb0b8]">
            Workspace
          </p>
          {navigation.map((item) => {
            const Icon = item.icon;
            if (item.enabled && item.to) {
              return (
                <NavLink
                  key={item.label}
                  to={item.to}
                  onClick={() => setSidebarOpen(false)}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-brand-50 text-brand-700 ring-1 ring-inset ring-brand-200"
                        : "text-[#747c8b] hover:bg-[#eff3f6] hover:text-[#252b3a]",
                    )
                  }
                >
                  <Icon className="size-[18px]" aria-hidden="true" />
                  {item.label}
                </NavLink>
              );
            }

            return (
              <div
                key={item.label}
                className="flex cursor-not-allowed items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-[#adb0b8]"
                aria-disabled="true"
              >
                <Icon className="size-[18px]" aria-hidden="true" />
                <span>{item.label}</span>
                <span className="ml-auto text-[10px] uppercase tracking-wider">
                  soon
                </span>
              </div>
            );
          })}
        </nav>

        <div className="border-t border-[#d1dbe5] p-4">
          <div className="flex items-center gap-3 rounded-lg bg-[#f4f8fa] p-3 ring-1 ring-inset ring-[#d1dbe5]">
            <div className="grid size-9 place-items-center rounded-full bg-[#2d3748] text-xs font-semibold text-brand-300">
              AD
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium text-[#252b3a]">
                Platform Admin
              </p>
              <p className="truncate text-xs text-[#747c8b]">初始化阶段</p>
            </div>
            <Settings className="size-4 text-[#747c8b]" aria-hidden="true" />
          </div>
        </div>
      </aside>

      <div className="lg:pl-72">
        <header className="sticky top-0 z-30 flex h-20 items-center gap-4 border-b border-[#d1dbe5] bg-white/95 px-4 backdrop-blur-xl sm:px-6 lg:px-8">
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={() => setSidebarOpen(true)}
            aria-label="打开导航"
          >
            <Menu className="size-5" />
          </Button>

          <div className="relative hidden max-w-md flex-1 md:block">
            <Search
              className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[#747c8b]"
              aria-hidden="true"
            />
            <input
              type="search"
              aria-label="搜索"
              disabled
              placeholder="搜索事件、资源或会话"
              className="h-10 w-full rounded-lg border border-[#d1dbe5] bg-[#f4f8fa] pl-10 pr-3 text-sm outline-none placeholder:text-[#adb0b8] disabled:cursor-not-allowed"
            />
          </div>

          <div className="ml-auto flex items-center gap-3">
            <div className="hidden items-center gap-2 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-700 sm:flex">
              <ShieldCheck className="size-3.5" aria-hidden="true" />
              只读安全模式
            </div>
            <Button
              variant="outline"
              className="hidden gap-2 sm:inline-flex"
              disabled
            >
              生产环境
              <ChevronDown className="size-4" aria-hidden="true" />
            </Button>
          </div>
        </header>

        <main className="min-h-[calc(100vh-5rem)] p-4 sm:p-6 lg:p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
