// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { register, uniqueActor } from "../support/actors";

test("V2: opt-in nickname discovery sends a real friend request without exposing private accounts", async ({ browser }) => {
  const aliceContext = await browser.newContext({ locale: "en-US" });
  const bobContext = await browser.newContext({ locale: "en-US" });
  try {
    const alice = await aliceContext.newPage();
    const bob = await bobContext.newPage();
    const aliceUser = uniqueActor("v2alice", "V2 Alice");
    const bobUser = uniqueActor("v2bob", "V2 Bob");
    await register(alice, aliceUser);
    await register(bob, bobUser);

    await alice.goto("/friends");
    await alice.getByRole("button", { name: "Find a friend" }).click();
    await alice.getByPlaceholder("Friend's handle").fill(bobUser.handle);
    await expect(alice.getByText("No opt-in matches found.")).toBeVisible();

    await bob.goto("/friends");
    await bob.getByRole("button", { name: "Find a friend" }).click();
    await bob.getByRole("checkbox", { name: /Allow people to find me/ }).check();
    await expect(bob.getByRole("checkbox", { name: /Allow people to find me/ })).toBeChecked();

    await alice.getByPlaceholder("Friend's handle").fill("");
    await alice.getByPlaceholder("Friend's handle").fill(bobUser.handle);
    const result = alice.locator(".dw-friend-discovery");
    await expect(result.getByText(bobUser.displayName)).toBeVisible();
    await result.getByRole("button", { name: "Add friend" }).click();
    await expect(alice.getByText("Friend request sent.")).toBeVisible();

    await bob.getByRole("checkbox", { name: /Allow people to find me/ }).uncheck();
    await expect(bob.getByRole("checkbox", { name: /Allow people to find me/ })).not.toBeChecked();
  } finally {
    await aliceContext.close();
    await bobContext.close();
  }
});
