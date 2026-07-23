import { Link } from "react-router-dom";

export function NotFoundPage() {
  return (
    <main className="grid min-h-screen place-items-center bg-slate-50 p-6 text-center">
      <div>
        <p className="text-sm font-semibold text-brand-700">404</p>
        <h1 className="mt-2 text-3xl font-semibold text-slate-950">
          页面不存在
        </h1>
        <p className="mt-3 text-sm text-slate-500">
          请求的页面尚未实现或地址有误。
        </p>
        <Link
          className="mt-6 inline-flex rounded-lg bg-slate-950 px-4 py-2 text-sm font-semibold text-white"
          to="/dashboard"
        >
          返回平台总览
        </Link>
      </div>
    </main>
  );
}
