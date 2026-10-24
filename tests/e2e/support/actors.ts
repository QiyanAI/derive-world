// SPDX-License-Identifier: Apache-2.0
import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";

export interface Actor {
  email: string;
  handle: string;
  displayName: string;
  password: string;
}

let sequence = 0;

export function uniqueActor(prefix: string, displayName?: string): Actor {
  sequence += 1;
  const stamp = `${Date.now().toString(36)}${sequence.toString(36)}${Math.random()
    .toString(36)
    .slice(2, 5)}`;
  const handle = `${prefix}${stamp}`
    .toLowerCase()
    .replace(/[^a-z0-9_-]/g, "")
    .slice(0, 32);
  return {
    email: `g05.${prefix}.${stamp}@example.test`,
    handle,
    displayName: displayName ?? `G05 ${prefix} ${stamp.slice(-5)}`,
    password: "correct horse battery staple"
  };
}

export async function fillRegistration(page: Page, actor: Actor): Promise<void> {
  await page.getByLabel("Email", { exact: true }).fill(actor.email);
  await page.getByLabel("Handle").fill(actor.handle);
  await page.getByLabel("Display name", { exact: true }).fill(actor.displayName);
  await page.getByLabel("Password").fill(actor.password);
}

export async function register(page: Page, actor: Actor): Promise<void> {
  await page.goto("/register");
  await fillRegistration(page, actor);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/welcome/);
}

export async function registerFromInvite(page: Page, actor: Actor): Promise<void> {
  await page.getByRole("link", { name: "Create an account" }).click();
  await expect(page).toHaveURL(/\/register\?next=/);
  await fillRegistration(page, actor);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/invite\//);
}

export async function login(page: Page, actor: Actor): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email", { exact: true }).fill(actor.email);
  await page.getByLabel("Password").fill(actor.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

export async function createCollaborative(
  page: Page,
  input: { title: string; summary: string; body: string }
): Promise<string> {
  await page.goto("/objects/new");
  await page.getByRole("radio", { name: /Collaborative/ }).check();
  await page.getByLabel("Title", { exact: true }).fill(input.title);
  await page.getByLabel("Summary", { exact: true }).fill(input.summary);
  await page.getByLabel("Body", { exact: true }).fill(input.body);
  await page.getByRole("button", { name: "Create object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
  return page.url().split("/objects/")[1].split("?")[0];
}

export async function createInteractive(
  page: Page,
  input: { title: string; prompt: string; options: string[] }
): Promise<string> {
  await page.goto("/objects/new");
  await page.getByRole("radio", { name: /Interactive/ }).check();
  await page.getByLabel("Title", { exact: true }).fill(input.title);
  await page.getByLabel("Prompt", { exact: true }).fill(input.prompt);
  let visibleOptions = 2;
  for (let index = 0; index < input.options.length; index += 1) {
    if (index >= visibleOptions) {
      await page.getByRole("button", { name: "Add option" }).click();
      visibleOptions += 1;
    }
    await page
      .getByLabel(`Option ${index + 1}`, { exact: true })
      .fill(input.options[index]!);
  }
  await page.getByRole("button", { name: "Create object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
  return page.url().split("/objects/")[1].split("?")[0];
}

export async function goToObjectTab(page: Page, objectID: string, tab: string): Promise<void> {
  await page.goto(`/objects/${objectID}?tab=${tab}`);
}

export async function editBody(page: Page, body: string): Promise<void> {
  await page.getByRole("button", { name: "Edit document" }).click();
  await page.getByRole("textbox", { name: "Body", exact: true }).fill(body);
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Saved.")).toBeVisible();
}

export async function createInvitation(
  page: Page,
  objectID: string,
  permissions: string[]
): Promise<string> {
  await goToObjectTab(page, objectID, "access");
  await expect(page.getByRole("heading", { name: "Invitations" })).toBeVisible();
  for (const permission of ["Read", "Contribute", "Derive", "Invite"]) {
    const checkbox = page.getByRole("checkbox", { name: permission, exact: true });
    if (permissions.includes(permission)) {
      await checkbox.check();
    } else {
      await checkbox.uncheck();
    }
  }
  await page.getByTestId("invite-create").click();
  const link = page.getByTestId("invite-link");
  await expect(link).toBeVisible();
  return link.inputValue();
}

export async function acceptInvitation(page: Page, link: string): Promise<void> {
  await page.goto(link);
  await expect(page.getByRole("button", { name: "Accept invitation" })).toBeVisible();
  await page.getByRole("button", { name: "Accept invitation" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
  await expect(page.getByText("You joined this object.")).toBeVisible();
}

export async function joinAsNewUser(
  browser: Browser,
  baseURL: string,
  actor: Actor,
  link: string
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  await page.goto(link);
  await expect(page.getByRole("heading", { name: "Invitation" })).toBeVisible();
  await registerFromInvite(page, actor);
  await acceptInvitation(page, link);
  return { context, page };
}

export async function acceptNextDialog(page: Page): Promise<void> {
  page.once("dialog", (dialog) => {
    void dialog.accept();
  });
}

export async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => {
    const innerWidth = window.innerWidth;
    const offenders = Array.from(document.querySelectorAll("*"))
      .map((element) => {
        const rect = element.getBoundingClientRect();
        return {
          tag: element.tagName,
          className: String(element.className ?? "").slice(0, 80),
          right: Math.round(rect.right * 10) / 10,
          width: Math.round(rect.width * 10) / 10,
          text: (element.textContent ?? "").trim().slice(0, 50)
        };
      })
      .filter((entry) => entry.right > innerWidth + 0.5)
      .slice(0, 12);
    return { scrollWidth: document.documentElement.scrollWidth, innerWidth, offenders };
  });
  if (overflow.scrollWidth > overflow.innerWidth + 1) {
    console.log("HORIZONTAL_OVERFLOW_OFFENDERS", JSON.stringify(overflow.offenders));
  }
  expect(
    overflow.scrollWidth,
    `horizontal overflow: ${overflow.scrollWidth} > ${overflow.innerWidth} at ${page.url()}`
  ).toBeLessThanOrEqual(overflow.innerWidth + 1);
}
