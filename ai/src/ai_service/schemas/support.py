"""Emergency fallback used only when normal LLM processing fails."""

import re

IMMEDIATE_SAFETY_ANSWER = ('Мне жаль, что вы оказались в такой ситуации. Если опасность непосредственная, обратитесь в местную '
 'экстренную службу и, если это безопасно, отойдите от того, что может причинить вред. Есть ли рядом '
 'человек, которого можно попросить побыть с вами?')


def needs_safety_fallback(message: str) -> bool:
    """Use an explicit danger signal when semantic routing is unavailable.

    Technical words such as "безопасно" and "kill" alone must not
    select a crisis answer.
    Healthy-model routing still interprets the complete original message.
    """
    for clause in re.split(r"[.!?;\n]|\b(?:но|but)\b", message.casefold()):
        if re.search(r"\bне\s+хочу\s+жить\b", clause):
            return True
        # Remove only a denied harmful action. Declining something unrelated
        # ("не хочу помощи") must not hide another explicit danger signal.
        clause = re.sub(
            r"\bне\s+(?:хочу|собираюсь|буду|планирую|намерен\w*)\s+(?:сейчас\s+)?"
            r"(?:(?:причин\w*|нанес\w*)\s+себе\s+вред|навред\w*\s+себе|"
            r"(?:убить|резать|порезать)\s+себя|(?:соверш\w*\s+)?(?:суицид\w*|самоубий\w*))|"
            r"\b(?:not|never|don't|do not)\s+(?:going to\s+)?(?:want|plan|intend)\s+to\s+"
            r"(?:kill|hurt|harm)\s+myself\b",
            "", clause,
        )
        if re.search(
            r"суицид|самоубий|поконч\w*\s+с\s+собой|"
            r"(?:причин\w*|нанес\w*)\s+себе\s+вред|навред\w*\s+себе|"
            r"(?:убить|убью|режу|порез\w*)\s+себя|"
            r"\b(?:мне|меня|нас|нам)\s+(?:сейчас\s+)?(?:угрож\w*|избива\w*|бьют)\b|"
            r"\bsuicid\w*\b|\b(?:kill|hurt|harm)\s+myself\b|\bwant to die\b",
            clause,
        ):
            return True
    return False
