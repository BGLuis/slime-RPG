# RPG Maker Event Codes Reference (MV/MZ)

This document describes the event command opcodes used in RPG Maker JSON data (Maps, CommonEvents, and Troops) and how the extraction pipeline processes them.

---

## 📝 Text and Dialogue Commands

| Opcode | Name | Description | Relevant Parameters |
| :--- | :--- | :--- | :--- |
| **101** | Show Text (Init) | Initializes a message window. | In MZ, parameter 4 is the Speaker Name. |
| **401** | Show Text (Line) | Contains a line of text for the message initiated by 101. | Parameter 0: String content. |
| **102** | Show Choices | Displays a list of options for the player. | Parameter 0: List of choice strings. |
| **402** | Choice Branch | Identifies the branch block for each choice. | Parameter 1: Choice text (used for reference). |
| **105** | Scrolling Text | Initializes scrolling text display. | - |
| **405** | Scrolling Text (Line) | Contains a line of scrolling text. | Parameter 0: String content. |

---

## ⚙️ Configuration and Data Commands

| Opcode | Name | Description | Relevant Parameters |
| :--- | :--- | :--- | :--- |
| **108** | Comment | Developer comment in the event. | Parameter 0: First line of comment. |
| **408** | Comment (Line) | Subsequent lines of a comment. | Parameter 0: String content. |
| **118** | Label | Defines a target label for "Jump to Label". | Parameter 0: Label name. |
| **122** | Control Variables | Modifies variable values. | May contain strings on direct assignment. |
| **320** | Change Name | Changes an actor's name. | Parameter 1: New name. |
| **324** | Change Nickname | Changes an actor's nickname. | Parameter 1: New nickname. |
| **325** | Change Profile | Changes an actor's profile / bio. | Parameter 1: New profile description. |

---

## 💻 Scripts and Plugin Commands

| Opcode | Name | Description | Handling |
| :--- | :--- | :--- | :--- |
| **355** | Script | Executes raw JavaScript code. | The extractor extracts only quoted strings within known display functions. |
| **655** | Script (Line) | Subsequent lines of JavaScript script. | Handled identically to 355. |
| **356** | Plugin Command (MV) | Plugin command for RPG Maker MV. | Extracts text from known text commands (e.g., `D_TEXT`, `mes`). |
| **357** | Plugin Command (MZ) | Plugin command for RPG Maker MZ. | Extracts values of non-technical keys from the parameter dictionary. |

---

## 🔍 In-Text Format Codes Reference

These escape sequences appear inside dialogue strings and are masked and preserved by the pipeline:

* **`\V[n]`**: Value of variable `n`.
* **`\N[n]`**: Name of actor `n`.
* **`\P[n]`**: Name of party member `n`.
* **`\G`**: Currency unit (Gold).
* **`\C[n]`**: Changes subsequent text color to color index `n`.
* **`\I[n]`**: Draws icon `n`.
* **`\M[id]`**: Reference to an external message (common in Japanese translation plugins like `Mgp_ExternMessage`).

---

## 📂 Object Attributes (Database JSON Metadata)

In addition to event commands, the extractor targets translatable text across database files (`Actors.json`, `Items.json`, `Skills.json`, `Weapons.json`, `Armors.json`, etc.):

* **`name`**: Name of the object / actor.
* **`description`**: Short item or skill description.
* **`note`**: Developer notes (may contain meta tags like `<Desc: ...>`).
* **`nickname`**: Actor nickname.
* **`profile`**: Actor biography / profile.
* **`message1 / message2`**: Battle messages (Skills / Items).

In **`System.json`**:
* **`gameTitle`**: Game title.
* **`terms`**: UI terms (Attributes, Battle Commands, Status Messages).
* **`elements`**: Element names (Fire, Ice, etc.).
* **`weaponTypes`**: Weapon category names.
