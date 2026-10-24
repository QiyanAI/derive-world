// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { expectNoHorizontalOverflow, uniqueActor } from "../support/actors";

test.describe("EN/ZH and 390px responsive reference world", () => {
  test("language, registration, object creation and core routes stay inside the viewport", async ({
    page
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const actor = uniqueActor("zhA");

    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "A living web of objects you and your AI can grow." })
    ).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expectNoHorizontalOverflow(page);

    await page.getByRole("button", { name: "中文" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
    await expect(
      page.getByRole("heading", { name: "一个由你与 AI 共同生长的活对象世界。" })
    ).toBeVisible();
    await expectNoHorizontalOverflow(page);

    await page.getByRole("link", { name: "创建账户", exact: true }).click();
    await page.getByLabel("邮箱", { exact: true }).fill(actor.email);
    await page.getByLabel("账号标识").fill(actor.handle);
    await page.getByLabel("显示名称", { exact: true }).fill(actor.displayName);
    await page.getByLabel("密码").fill(actor.password);
    await page.getByRole("button", { name: "创建账户" }).click();
    await expect(page).toHaveURL(/\/welcome/);
    await expect(page.getByRole("heading", { name: "你的数字自我已就绪" })).toBeVisible();
    await expectNoHorizontalOverflow(page);

    await page.getByRole("link", { name: "创建第一个对象" }).click();
    await page.getByRole("radio", { name: /协作对象/ }).check();
    await page.getByLabel("标题", { exact: true }).fill("中文响应式对象");
    await page.getByLabel("摘要", { exact: true }).fill("在手机宽度下仍可完整操作。");
    await page.getByLabel("正文", { exact: true }).fill("这是一个真实的协作对象。");
    await page.getByRole("button", { name: "创建对象" }).click();
    await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
    await expect(page.getByRole("heading", { name: "中文响应式对象" })).toBeVisible();
    await expectNoHorizontalOverflow(page);

    for (const route of ["/", "/objects", "/ai", "/self", "/settings", "/objects/new"]) {
      await page.goto(route);
      await expectNoHorizontalOverflow(page);
      await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
    }

    // Switch back without losing the session or leaving overflowing furniture.
    await page.getByRole("button", { name: "English" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expectNoHorizontalOverflow(page);
  });
});
