import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";

import { DashboardPage } from "@/pages/dashboard-page";
import { LoginPage } from "@/pages/login-page";

describe("LoginPage", () => {
  it("renders the login form and explains the current scaffold state", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText("用户名"), "admin");
    await user.type(screen.getByLabelText("密码"), "not-a-real-password");
    await user.click(screen.getByRole("button", { name: /登录平台/ }));

    expect(screen.getByRole("status")).toHaveTextContent(
      "认证能力将在 Task 1.1 接入",
    );
  });
});

describe("DashboardPage", () => {
  it("does not present placeholder metrics as production data", () => {
    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "平台总览" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("当前处于工程初始化阶段，未接入生产数据。", {
        exact: false,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("暂无分析记录")).toBeInTheDocument();
  });
});
