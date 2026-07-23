import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import {
  Activity,
  ArrowRight,
  CheckCircle2,
  LockKeyhole,
  ShieldCheck,
  Sparkles,
} from "lucide-react";

import { login } from "@/api/auth";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function LoginPage() {
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setNotice("");
    const form = new FormData(event.currentTarget);
    try {
      await login({
        username: String(form.get("username") ?? ""),
        password: String(form.get("password") ?? ""),
      });
      navigate("/knowledge");
    } catch {
      setNotice("登录失败，请检查用户名、密码或账号状态。");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="relative min-h-screen overflow-hidden bg-[#252b3a] px-4 py-8 text-white sm:px-6 lg:px-8">
      <div className="relative mx-auto grid min-h-[calc(100vh-4rem)] max-w-7xl items-center gap-12 lg:grid-cols-[1.1fr_0.9fr]">
        <section className="hidden max-w-2xl lg:block">
          <div className="mb-10 inline-flex items-center gap-3">
            <div className="grid size-11 place-items-center rounded-lg bg-brand-400/15 text-brand-300 ring-1 ring-brand-300/25">
              <Activity className="size-6" aria-hidden="true" />
            </div>
            <div>
              <p className="font-semibold tracking-wide">AI Native AIOps</p>
              <p className="text-xs text-slate-400">
                Evidence-backed operations intelligence
              </p>
            </div>
          </div>

          <p className="mb-5 text-sm font-semibold uppercase tracking-[0.2em] text-brand-300">
            Operational clarity, built in
          </p>
          <h1 className="max-w-xl text-5xl font-semibold leading-[1.12] tracking-tight text-white">
            让每一次故障分析，
            <span className="text-brand-300">都有证据可循。</span>
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-8 text-slate-400">
            统一关联日志、指标、告警、Kubernetes
            与知识库，为运维团队提供可解释、可审计的只读分析体验。
          </p>

          <div className="mt-10 grid max-w-xl gap-4 sm:grid-cols-3">
            {[
              {
                icon: ShieldCheck,
                title: "默认只读",
                text: "不自动修改生产环境",
              },
              { icon: Sparkles, title: "证据优先", text: "结论关联真实来源" },
              {
                icon: CheckCircle2,
                title: "全程审计",
                text: "调用过程清晰可追踪",
              },
            ].map((item) => (
              <div
                key={item.title}
                className="rounded-xl border border-white/10 bg-white/[0.035] p-4 backdrop-blur-sm"
              >
                <item.icon
                  className="mb-3 size-5 text-brand-300"
                  aria-hidden="true"
                />
                <p className="text-sm font-semibold text-slate-100">
                  {item.title}
                </p>
                <p className="mt-1 text-xs leading-5 text-slate-500">
                  {item.text}
                </p>
              </div>
            ))}
          </div>
        </section>

        <section className="mx-auto w-full max-w-md">
          <div className="mb-8 flex items-center gap-3 lg:hidden">
            <div className="grid size-10 place-items-center rounded-lg bg-brand-400/15 text-brand-300">
              <Activity className="size-5" aria-hidden="true" />
            </div>
            <p className="font-semibold">AI Native AIOps</p>
          </div>

          <Card className="border-white/10 bg-white/[0.98] text-slate-950 shadow-[0_30px_90px_rgba(0,0,0,.38)]">
            <CardHeader className="space-y-3 p-7 pb-4 sm:p-8 sm:pb-5">
              <div className="grid size-11 place-items-center rounded-lg bg-[#2d3748] text-brand-300">
                <LockKeyhole className="size-5" aria-hidden="true" />
              </div>
              <div>
                <CardTitle className="text-2xl">欢迎回来</CardTitle>
                <CardDescription className="mt-2">
                  使用平台账户进入智能运维工作台
                </CardDescription>
              </div>
            </CardHeader>
            <CardContent className="p-7 pt-3 sm:p-8 sm:pt-3">
              <form className="space-y-5" onSubmit={handleSubmit}>
                <div className="space-y-2">
                  <Label htmlFor="username">用户名</Label>
                  <Input
                    id="username"
                    name="username"
                    autoComplete="username"
                    placeholder="请输入用户名"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <Label htmlFor="password">密码</Label>
                    <span className="text-xs text-slate-400">
                      请联系管理员重置
                    </span>
                  </div>
                  <Input
                    id="password"
                    name="password"
                    type="password"
                    autoComplete="current-password"
                    placeholder="请输入密码"
                    required
                  />
                </div>
                <Button
                  type="submit"
                  size="lg"
                  className="w-full"
                  disabled={loading}
                >
                  {loading ? "登录中..." : "登录平台"}
                  <ArrowRight className="size-4" aria-hidden="true" />
                </Button>
                {notice ? (
                  <p
                    role="status"
                    className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800"
                  >
                    {notice}
                  </p>
                ) : null}
              </form>
              <p className="mt-6 border-t border-slate-100 pt-5 text-center text-xs leading-5 text-slate-400">
                登录即表示你同意遵守平台只读安全策略与审计规范
              </p>
            </CardContent>
          </Card>
        </section>
      </div>
    </main>
  );
}
