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
    await expect(bob.getByRole("checkbox", { name: /Allow people to find me/ })).toBeEnabled();

    await alice.getByPlaceholder("Friend's handle").fill("");
    await alice.getByPlaceholder("Friend's handle").fill(bobUser.handle);
    const result = alice.locator(".dw-friend-discovery");
    await expect(result.getByText(bobUser.displayName)).toBeVisible();
    await result.getByRole("button", { name: "Add friend" }).click();
    await expect(alice.getByText("Friend request sent.")).toBeVisible();

    await bob.goto("/friends");
    await bob.getByRole("button", { name: "Accept" }).click();
    await expect(bob.locator(".dw-friend-list").getByText(aliceUser.displayName)).toBeVisible();

    await bob.getByRole("checkbox", { name: /Allow people to find me/ }).uncheck();
    await expect(bob.getByRole("checkbox", { name: /Allow people to find me/ })).not.toBeChecked();
    await expect(bob.getByRole("checkbox", { name: /Allow people to find me/ })).toBeEnabled();

    await alice.goto("/objects/new?template=living_scene&purpose=project&first=1");
    await alice.getByRole("button", { name: "Create object" }).click();
    await expect(alice).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
    const worldURL = new URL(alice.url());
    await alice.goto(worldURL.pathname + "?tab=access");
    const inviteSelect = alice.getByLabel("Choose friend");
    await expect(inviteSelect).toBeVisible();
    const friendOption = inviteSelect.getByRole("option", { name: new RegExp(bobUser.handle) });
    const friendValue = await friendOption.getAttribute("value");
    expect(friendValue).toBeTruthy();
    await inviteSelect.selectOption(friendValue!);
    await alice.getByTestId("invite-accepted-friend").click();
    await expect(alice.getByText("Invitation sent by private message; access requires acceptance.")).toBeVisible();

    await bob.goto("/friends");
    await bob.locator(".dw-friend-list").getByRole("button", { name: new RegExp(aliceUser.displayName) }).click();
    const invitationMessage = bob.getByText(/Join my world: .*\/invite\//).first();
    await expect(invitationMessage).toBeVisible();
    const messageText = await invitationMessage.textContent();
    const invitationURL = messageText?.match(/https?:\/\/\S+\/invite\/[a-zA-Z0-9_-]+/)?.[0];
    expect(invitationURL).toBeTruthy();
    await bob.goto(invitationURL!);
    await bob.getByRole("button", { name: "Accept invitation" }).click();
    await expect(bob).toHaveURL(/\/objects\/[0-9a-f-]{36}/);

  } finally {
    await aliceContext.close();
    await bobContext.close();
  }
});
