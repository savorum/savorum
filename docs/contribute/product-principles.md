# Product principles

Feature proposals are judged against these principles. Each names a tradeoff
Savorum makes and why.

Savorum is a recipe manager for keeping and using a permanent recipe library.
Importing, organizing, finding, reading, editing, scaling, cooking, printing,
and exporting recipes take priority over adjacent workflows.

## Self-hosting stays simple

One application, SQLite, and one persistent data area must suffice. Redis,
PostgreSQL, queue brokers, external identity systems, and hosted services are
never mandatory. A household should keep its cookbook running and backed up
without operating other services, so features needing more infrastructure are
optional or left out.

## A single cook sees no account machinery

Local mode, with authentication disabled, is a supported way to use Savorum.
Requests resolve to one persisted local user and one default library, and
accounts, roles, and library selection disappear from ordinary recipe
workflows. Household sharing is available when wanted, with the library as the
ownership and authorization boundary. Identity exists to protect and attribute
access to recipes, so sign-in methods, tokens, and provisioning stay optional
and never become setup for a local cookbook.

## The authored recipe is authoritative

Savorum preserves what the author wrote, even when a structured reading would be
more convenient. Inference keeps the original wording and states its
uncertainty rather than replacing the text. Scaling, conversion, and image
derivatives change what is displayed, never the stored recipe. Original content
and assets stay recoverable and portable. A library meant to last can't depend
on today's parser being right.

## A rich model with a plain surface

The recipe model is detailed so nothing is lost, yet stays out of the way.
Ingredients and instructions dominate reading and editing. Uncommon metadata
uses progressive disclosure, and unconfigured features disappear from ordinary
screens. Visible controls must work, errors must support recovery, and keyboard
and narrow-screen use are first-class. The
[core UX contract](core-ux-contract.md) makes this testable.

## Exchange is separate from the library

SREF defines how recipes move between programs, not Savorum's interface or
storage. Portable recipe semantics stay separate from tags, books, permissions,
import events, cooking history, presentation preferences, and other library
state, so an export carries only what makes sense outside one library.

## Non-goals

Savorum is not a general household inventory system, calendar,
identity-management platform, workflow engine, plugin or runtime host, food
ontology, nutrition-analysis engine, or SaaS infrastructure framework. Meal
planning, calendar feeds, shopping lists, the pantry, and integrations support
cooking from the library, stay secondary to recipe workflows, and never become
required setup. Crossing these boundaries requires an explicit product
decision.

## Evaluating a proposal

A feature proposal should answer:

1. Which core recipe task improves, and what observable user outcome
   demonstrates it?
2. What does a single-user installation see, and what disappears if
   unconfigured?
3. Does it preserve authored information and the recipe/library boundary?
4. Can it run within the simple self-hosted architecture?
5. Which regression test protects the outcome, and which non-goal does it
   approach?

An exception must name the principle, explain why the benefit warrants crossing
it, and get an explicit product decision before it expands the contract.
