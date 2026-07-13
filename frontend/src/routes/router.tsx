import { Navigate, createBrowserRouter } from "react-router-dom";

import { AppShell } from "@/components/layout/app-shell";
import { AnalysisPage } from "@/pages/analysis-page";
import { DashboardPage } from "@/pages/dashboard-page";
import { KnowledgePage } from "@/pages/knowledge-page";
import { LoginPage } from "@/pages/login-page";
import { NotFoundPage } from "@/pages/not-found-page";
import { OperationsPage } from "@/pages/operations-page";
import { SettingsPage } from "@/pages/settings-page";
import { WorkflowPage } from "@/pages/workflow-page";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Navigate to="/login" replace />,
  },
  {
    path: "/login",
    element: <LoginPage />,
  },
  {
    element: <AppShell />,
    children: [
      { path: "/dashboard", element: <DashboardPage /> },
      { path: "/analysis", element: <AnalysisPage /> },
      { path: "/knowledge", element: <KnowledgePage /> },
      { path: "/workflows", element: <WorkflowPage /> },
      { path: "/operations", element: <OperationsPage /> },
      { path: "/settings", element: <SettingsPage /> },
    ],
  },
  {
    path: "*",
    element: <NotFoundPage />,
  },
]);
