from pathlib import Path
scope = {}
exec(Path('/workspace/solution.py').read_text(encoding='utf-8'), scope)
assert scope['TASK_ID'] == 'refactor-core-001'
assert scope['STATUS'] == 'STABLE'
print('1 passed')
