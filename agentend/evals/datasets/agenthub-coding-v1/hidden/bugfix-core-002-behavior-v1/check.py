from pathlib import Path
scope = {}
exec(Path('/workspace/solution.py').read_text(encoding='utf-8'), scope)
assert scope['TASK_ID'] == 'bugfix-core-002'
assert scope['STATUS'] == 'FIXED'
print('1 passed')
